// SPDX-License-Identifier: MIT OR Apache-2.0

package outputs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/khulnasoft/fanal/internal/pkg/batcher"
	"github.com/khulnasoft/fanal/internal/pkg/utils"
	"github.com/khulnasoft/fanal/types"
)

type eSPayload struct {
	types.KhulnasoftPayload
	Timestamp time.Time `json:"@timestamp"`
}

type esResponse struct {
	Error struct {
		Type   string `json:"type"`
		Reason string `json:"reason"`
	} `json:"error"`
	Status int `json:"status"`
}

type esBulkResponse struct {
	Errors bool             `json:"errors"`
	Items  []esItemResponse `json:"items"`
}

type esItemResponse struct {
	Create esResponse `json:"create"`
}

func NewElasticsearchClient(params types.InitClientArgs) (*Client, error) {
	esCfg := params.Config.Elasticsearch
	endpointUrl := fmt.Sprintf("%s/%s/%s", esCfg.HostPort, esCfg.Index, esCfg.Type)
	c, err := NewClient("Elasticsearch", endpointUrl, esCfg.CommonConfig, params)
	if err != nil {
		return nil, err
	}

	if esCfg.Batching.Enabled {
		utils.Log(utils.InfoLvl, c.OutputType, fmt.Sprintf("Batching enabled: %v max bytes, %v interval", esCfg.Batching.BatchSize, esCfg.Batching.FlushInterval))
		callbackFn := func(khulnasoftPayloads []types.KhulnasoftPayload, data []byte) {
			go c.elasticsearchPost("", data, khulnasoftPayloads...)
		}
		c.batcher = batcher.New(
			batcher.WithBatchSize(esCfg.Batching.BatchSize),
			batcher.WithFlushInterval(esCfg.Batching.FlushInterval),
			batcher.WithMarshal(c.marshalESBulkPayload),
			batcher.WithCallback(callbackFn),
		)
	}
	if esCfg.EnableCompression {
		c.EnableCompression = true
		utils.Log(utils.InfoLvl, c.OutputType, "Compression enabled")
	}

	return c, nil
}

func (c *Client) ElasticsearchPost(khulnasoftpayload types.KhulnasoftPayload) {
	if c.Config.Elasticsearch.Batching.Enabled {
		c.batcher.Push(khulnasoftpayload)
		return
	}

	payload, err := c.marshalESPayload(khulnasoftpayload)
	if err != nil {
		utils.Log(utils.ErrorLvl, c.OutputType, fmt.Sprintf("Failed to marshal payload: %v", err))
	}

	c.elasticsearchPost(c.getIndex(), payload, khulnasoftpayload)
}

var esReasonMappingFieldsRegex *regexp.Regexp = regexp.MustCompile(`\[\w+(\.\w+)+\]`)

// ElasticsearchPost posts event to Elasticsearch
func (c *Client) elasticsearchPost(index string, payload []byte, khulnasoftPayloads ...types.KhulnasoftPayload) {
	sz := int64(len(khulnasoftPayloads))
	c.Stats.Elasticsearch.Add(Total, sz)

	var eURL string
	if index == "" {
		eURL = c.Config.Elasticsearch.HostPort + "/_bulk"
	} else {
		eURL = c.Config.Elasticsearch.HostPort + "/" + index + "/" + c.Config.Elasticsearch.Type
	}

	endpointURL, err := url.Parse(eURL)
	if err != nil {
		c.setElasticSearchErrorMetrics(sz)
		utils.Log(utils.ErrorLvl, c.OutputType, err.Error())
		return
	}

	reqOpts := []RequestOptionFunc{
		// Set request headers
		func(req *http.Request) {
			if c.Config.Elasticsearch.ApiKey != "" {
				req.Header.Set("Authorization", "APIKey "+c.Config.Elasticsearch.ApiKey)
			}

			if c.Config.Elasticsearch.Username != "" && c.Config.Elasticsearch.Password != "" {
				req.SetBasicAuth(c.Config.Elasticsearch.Username, c.Config.Elasticsearch.Password)
			}

			for i, j := range c.Config.Elasticsearch.CustomHeaders {
				req.Header.Set(i, j)
			}
		},

		// Set the final endpointURL
		func(req *http.Request) {
			// Append pipeline parameter to the URL if configured
			if c.Config.Elasticsearch.Pipeline != "" {
				query := endpointURL.Query()
				query.Set("pipeline", c.Config.Elasticsearch.Pipeline)
				endpointURL.RawQuery = query.Encode()
			}
			// Set request URL
			req.URL = endpointURL
		},
	}

	var response string
	if c.Config.Elasticsearch.Batching.Enabled {
		// Use PostWithResponse call when batching is enabled in order to capture response body on 200
		res, err := c.PostWithResponse(payload, reqOpts...)
		if err != nil {
			response = err.Error()
		} else {
			response = res
		}
	} else {
		// Use regular Post call, this avoid parsing response on http status 200
		err = c.Post(payload, reqOpts...)
		if err != nil {
			response = err.Error()
		}
	}

	if response != "" {
		if c.Config.Elasticsearch.Batching.Enabled {
			var resp esBulkResponse
			if err2 := json.Unmarshal([]byte(response), &resp); err2 != nil {
				c.setElasticSearchErrorMetrics(sz)
				return
			}
			if len(resp.Items) != len(khulnasoftPayloads) {
				utils.Log(utils.ErrorLvl, c.OutputType, fmt.Sprintf("mismatched %v responses with %v request payloads", len(resp.Items), len(khulnasoftPayloads)))
				c.setElasticSearchErrorMetrics(sz)
				return
			}
			// Check errors. Not using the mapping errors retry approach for batched/bulk requests
			// Only mark set the errors and stats
			if resp.Errors {
				failed := int64(0)
				for _, item := range resp.Items {
					switch item.Create.Status {
					case http.StatusOK, http.StatusCreated:
					default:
						failed++
					}
				}
				c.setElasticSearchErrorMetrics(failed)
				// Set success sz that is reported at the end of this function
				sz -= failed
			}
		} else {
			// Slightly refactored the original approach to mapping errors, but logic is still the same
			// The Request is retried only once without the field that can't be mapped.
			// One of the problems with this approach is that if the mapping has two "unmappable" fields
			// only the first one is returned with the error and removed from the retried request.
			// Do we need to retry without the field? Do we need to keep retrying and removing fields until it succeeds?
			var resp esResponse
			if err2 := json.Unmarshal([]byte(response), &resp); err2 != nil {
				c.setElasticSearchErrorMetrics(sz)
				return
			}

			payload := khulnasoftPayloads[0]

			if resp.Error.Type == "document_parsing_exception" {
				k := esReasonMappingFieldsRegex.FindStringSubmatch(resp.Error.Reason)
				if len(k) == 0 {
					c.setElasticSearchErrorMetrics(sz)
					return
				}
				if !strings.Contains(k[0], "output_fields") {
					c.setElasticSearchErrorMetrics(sz)
					return
				}
				s := strings.ReplaceAll(k[0], "[output_fields.", "")
				s = strings.ReplaceAll(s, "]", "")
				for i := range payload.OutputFields {
					if strings.HasPrefix(i, s) {
						delete(payload.OutputFields, i)
					}
				}
				utils.Log(utils.InfoLvl, c.OutputType, "attempt to POST again the payload without the wrong field")
				err = c.Post(payload, reqOpts...)
				if err != nil {
					c.setElasticSearchErrorMetrics(sz)
					return
				}
			}
		}
	}

	// Setting the success status
	go c.CountMetric(Outputs, sz, []string{"output:elasticsearch", "status:ok"})
	c.Stats.Elasticsearch.Add(OK, sz)
	c.PromStats.Outputs.With(map[string]string{"destination": "elasticsearch", "status": OK}).Add(float64(sz))
	c.OTLPMetrics.Outputs.With(attribute.String("destination", "elasticsearch"),
		attribute.String("status", OK)).Inc()
}

func (c *Client) ElasticsearchCreateIndexTemplate(config types.ElasticsearchOutputConfig) error {
	d := c
	indexExists, err := c.isIndexTemplateExist(config)
	if err != nil {
		utils.Log(utils.ErrorLvl, c.OutputType, err.Error())
		return err
	}
	if indexExists {
		utils.Log(utils.InfoLvl, c.OutputType, "Index template already exists")
		return nil
	}

	pattern := "-*"
	if config.Suffix == None {
		pattern = ""
	}
	m := strings.ReplaceAll(ESmapping, "${INDEX}", config.Index)
	m = strings.ReplaceAll(m, "${PATTERN}", pattern)
	m = strings.ReplaceAll(m, "${SHARDS}", fmt.Sprintf("%v", config.NumberOfShards))
	m = strings.ReplaceAll(m, "${REPLICAS}", fmt.Sprintf("%v", config.NumberOfReplicas))
	j := make(map[string]interface{})
	if err := json.Unmarshal([]byte(m), &j); err != nil {
		utils.Log(utils.ErrorLvl, c.OutputType, err.Error())
		return err
	}
	// create the index template by PUT
	if err := d.Put(j); err != nil {
		utils.Log(utils.ErrorLvl, c.OutputType, err.Error())
		return err
	}

	utils.Log(utils.InfoLvl, c.OutputType, "Index template created")
	return nil
}

func (c *Client) isIndexTemplateExist(config types.ElasticsearchOutputConfig) (bool, error) {
	clientCopy := c
	var err error
	u, err := url.Parse(fmt.Sprintf("%s/_index_template/khulnasoft", config.HostPort))
	if err != nil {
		return false, err
	}
	clientCopy.EndpointURL = u
	if err := clientCopy.Get(); err != nil {
		if err.Error() == "resource not found" {
			return false, nil
		}
	}
	return true, nil
}

// setElasticSearchErrorMetrics set the error stats
func (c *Client) setElasticSearchErrorMetrics(n int64) {
	go c.CountMetric(Outputs, n, []string{"output:elasticsearch", "status:error"})
	c.Stats.Elasticsearch.Add(Error, n)
	c.PromStats.Outputs.With(map[string]string{"destination": "elasticsearch", "status": Error}).Add(float64(n))
	c.OTLPMetrics.Outputs.With(attribute.String("destination", "elasticsearch"),
		attribute.String("status", Error)).Inc()
}

func (c *Client) buildESPayload(khulnasoftpayload types.KhulnasoftPayload) eSPayload {
	payload := eSPayload{KhulnasoftPayload: khulnasoftpayload, Timestamp: khulnasoftpayload.Time}

	if c.Config.Elasticsearch.FlattenFields || c.Config.Elasticsearch.CreateIndexTemplate {
		for i, j := range payload.OutputFields {
			if strings.Contains(i, ".") {
				payload.OutputFields[strings.ReplaceAll(i, ".", "_")] = j
				delete(payload.OutputFields, i)
			}
		}
	}
	return payload
}

func (c *Client) marshalESPayload(khulnasoftpayload types.KhulnasoftPayload) ([]byte, error) {
	return json.Marshal(c.buildESPayload(khulnasoftpayload))
}

func (c *Client) marshalESBulkPayload(khulnasoftpayload types.KhulnasoftPayload) ([]byte, error) {
	body, err := c.marshalESPayload(khulnasoftpayload)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	_, _ = buf.WriteString(`{"create":{`)
	_, _ = buf.WriteString(`"_index":"`)
	_, _ = buf.WriteString(c.getIndex())
	_, _ = buf.WriteString("\"}}\n")

	_, _ = buf.Write(body)
	_, _ = buf.WriteRune('\n')

	return buf.Bytes(), nil
}

func (c *Client) getIndex() string {
	var index string

	current := time.Now()
	switch c.Config.Elasticsearch.Suffix {
	case None:
		index = c.Config.Elasticsearch.Index
	case "monthly":
		index = c.Config.Elasticsearch.Index + "-" + current.Format("2006.01")
	case "annually":
		index = c.Config.Elasticsearch.Index + "-" + current.Format("2006")
	default:
		index = c.Config.Elasticsearch.Index + "-" + current.Format("2006.01.02")
	}
	return index
}
