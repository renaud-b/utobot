package network

import (
	"net/http"

	"github.com/stretchr/testify/mock"
)

type HttpClient struct {
	client http.Client
	url    string
}

// NewHttpClient creates a new instance of HttpClient with the provided URL.
func NewHttpClient(url string) *HttpClient {
	return &HttpClient{
		client: http.Client{},
		url:    url,
	}
}

type Mock struct {
	mock.Mock
}
