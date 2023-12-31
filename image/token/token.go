package token

import (
	"context"

	"github.com/google/go-containerregistry/pkg/authn"

	"github.com/khulnasoft-lab/fanal/image/token/azure"
	"github.com/khulnasoft-lab/fanal/image/token/ecr"
	"github.com/khulnasoft-lab/fanal/image/token/google"
	"github.com/khulnasoft-lab/fanal/log"
	"github.com/khulnasoft-lab/fanal/types"
)

var (
	registries []Registry
)

func init() {
	RegisterRegistry(&google.Registry{})
	RegisterRegistry(&ecr.ECR{})
	RegisterRegistry(&azure.Registry{})
}

type Registry interface {
	CheckOptions(domain string, option types.DockerOption) error
	GetCredential(ctx context.Context) (string, string, error)
}

func RegisterRegistry(registry Registry) {
	registries = append(registries, registry)
}

func GetToken(ctx context.Context, domain string, opt types.DockerOption) (auth authn.Basic) {
	if opt.UserName != "" || opt.Password != "" {
		return authn.Basic{Username: opt.UserName, Password: opt.Password}
	}

	// check registry which particular to get credential
	for _, registry := range registries {
		err := registry.CheckOptions(domain, opt)
		if err != nil {
			continue
		}
		username, password, err := registry.GetCredential(ctx)
		if err != nil {
			// only skip check registry if error occurred
			log.Logger.Debug(err)
			break
		}
		return authn.Basic{Username: username, Password: password}
	}
	return authn.Basic{}
}
