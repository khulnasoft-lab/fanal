package licensing

import (
	"fmt"
	"io/fs"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/khulnasoft-lab/fanal/licensing/config"
	"github.com/khulnasoft-lab/fanal/types"
	"github.com/liamg/memoryfs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func Test_LicenseScanning(t *testing.T) {

	tests := []struct {
		name             string
		filePath         string
		expectLicense    bool
		packageName      string
		expectedFindings []types.LicenseFinding
		scanConfig       *config.Config
	}{
		{
			name:          "C file with AGPL-3.0",
			filePath:      "testdata/licensed.c",
			expectLicense: true,
			expectedFindings: []types.LicenseFinding{
				{
					License:                     "AGPL-3.0",
					MatchType:                   "Header",
					GoogleLicenseClassification: "forbidden",
					Confidence:                  0.98,
					StartLine:                   2,
					EndLine:                     13,
				},
			},
		},
		{
			name:             "C file with AGPL-3.0 with 100 confidence",
			filePath:         "testdata/licensed.c",
			expectLicense:    false,
			expectedFindings: []types.LicenseFinding{},
			scanConfig: &config.Config{
				MatchConfidenceThreshold: 1.0,
				IncludeHeaders:           true,
				RiskThreshold:            10,
			},
		},
		{
			name:             "C file with ignored AGPL-3.0",
			filePath:         "testdata/licensed.c",
			expectLicense:    false,
			expectedFindings: []types.LicenseFinding{},
			scanConfig: &config.Config{
				IgnoredLicences: []string{
					"AGPL-3.0",
				},
			},
		},
		{
			name:          "C file with no license",
			filePath:      "testdata/unlicensed.c",
			expectLicense: false,
		},
		{
			name:          "Creative commons License file",
			filePath:      "testdata/LICENSE_creativecommons",
			expectLicense: true,
			expectedFindings: []types.LicenseFinding{
				{
					License:                     "Commons-Clause",
					MatchType:                   "License",
					GoogleLicenseClassification: "forbidden",
					Confidence:                  0.98,
					StartLine:                   1,
					EndLine:                     13,
				},
			},
		},
		{
			name:          "Package folder identifies package",
			filePath:      "testdata/callsites",
			expectLicense: true,
			packageName:   "callsites",
			expectedFindings: []types.LicenseFinding{
				{
					License:                     "MIT",
					MatchType:                   "License",
					GoogleLicenseClassification: "notice",
					Confidence:                  0.98,
					StartLine:                   5,
					EndLine:                     9,
				},
			},
			scanConfig: &config.Config{
				RiskThreshold: 10,
			},
		},
		{
			name:          "Apache 2 License file",
			filePath:      "testdata/LICENSE_apache2",
			expectLicense: false,
		},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%#v", tt.name), func(t *testing.T) {

			configPath := ""
			var err error

			if tt.scanConfig != nil {
				configPath, err = createTestConfig(t, tt.scanConfig)
				require.NoError(t, err)
			}

			scanner, err := NewScanner(configPath)
			require.NoError(t, err)

			var testFS fs.FS
			f, err := os.Stat(tt.filePath)
			require.NoError(t, err)
			if f.IsDir() {
				testFS = os.DirFS(tt.filePath)
			} else {
				memfs := memoryfs.New()
				if filepath.Dir(tt.filePath) != "." {
					err = memfs.MkdirAll(filepath.Dir(tt.filePath), os.ModePerm)
					require.NoError(t, err)
				}
				content, err := os.ReadFile(tt.filePath)
				require.NoError(t, err)
				err = memfs.WriteFile(tt.filePath, content, os.ModePerm)
				testFS = memfs
			}

			licenses, err := scanner.ScanFS(testFS)
			require.NoError(t, err)

			if tt.expectLicense {
				assert.NotNil(t, licenses)
				require.Len(t, licenses, 1)
				license := licenses[0]
				assert.Len(t, license.Findings, len(tt.expectedFindings))
				for i, f := range tt.expectedFindings {
					lf := license.Findings[i]
					assert.Equal(t, f.License, lf.License)
					assert.Equal(t, f.MatchType, lf.MatchType)
					assert.Equal(t, f.StartLine, lf.StartLine)
					assert.Equal(t, f.EndLine, lf.EndLine)
					assert.Equal(t, f.GoogleLicenseClassification, lf.GoogleLicenseClassification)
					assert.Greater(t, lf.Confidence, 0.8)
				}
			}
		})

	}
}

func createTestConfig(t *testing.T, scanConfig *config.Config) (string, error) {
	configFile := filepath.Join(t.TempDir(), "scanConfig.yaml")
	content, err := yaml.Marshal(scanConfig)
	if err != nil {
		return "", err
	}
	return configFile, ioutil.WriteFile(configFile, content, os.ModePerm)
}
