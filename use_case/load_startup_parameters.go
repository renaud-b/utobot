package use_case

import (
	"utobot/domain"
)

type ParametersLoader interface {
	Load(path string) (domain.Parameters, error)
}

type LoadStartupParameters struct {
	Loader ParametersLoader
}

func NewLoadStartupParameters(loader ParametersLoader) LoadStartupParameters {
	return LoadStartupParameters{Loader: loader}
}

func (uc LoadStartupParameters) Execute(configPath string, runtimeOverride string) (domain.Parameters, error) {
	params, err := uc.Loader.Load(configPath)
	if err != nil {
		return domain.Parameters{}, err
	}

	if runtimeOverride != "" {
		params.RuntimeChainID = runtimeOverride
	}

	if err := params.Validate(); err != nil {
		return domain.Parameters{}, err
	}

	return params, nil
}
