package main

import (
	"crypto/tls"
	"golang.org/x/net/http2"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"time"
)

const ()

type (
	HttpClient struct {
		*http.Client
	}
)

func httpClient() (*HttpClient, error) {
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   15 * time.Second,
			KeepAlive: 50 * time.Second,
		}).DialContext,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
		TLSHandshakeTimeout:   5 * time.Second,
		MaxConnsPerHost:       2,
		ResponseHeaderTimeout: 5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	if err := http2.ConfigureTransport(transport); err != nil {
		return nil, err
	}

	// Do not follow redirects:
	checkRedirect := func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}

	return &HttpClient{
		&http.Client{
			Transport:     transport,
			CheckRedirect: checkRedirect,
		},
	}, nil
}

func (httpClient HttpClient) postData(
	log *SimpleLogger,
	url string,
	headers *map[string]string,
	data io.Reader,
) {

	defaultUserAgent := "FLB/go-odp (github.com/JamesJJ/fluent-bit-output-deduplicated-post)"
	request, err := http.NewRequest("POST", url, data)
	request.Header.Set("User-Agent", defaultUserAgent)
	if headers != nil {
		for hk, hv := range *headers {
			request.Header.Set(hk, hv)
		}
	}
	resp, err := httpClient.Do(request)
	if err != nil {
		log.Error.Printf(
			"HTTP request failed: %#v\n",
			err,
		)
	} else {
		log.Debug.Printf(
			"HTTP response object: %#v\n",
			resp,
		)
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Error.Printf(
				"HTTP body read error: %#v\n",
				err,
			)
		}
		log.Debug.Printf(
			"HTTP response body: %#v\n",
			string(body),
		)
	}
	if resp.StatusCode >= 400 {
		log.Error.Printf(
			"HTTP response not ok: %#v\n",
			resp,
		)
	}
}
