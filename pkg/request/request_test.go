// stream-http-go - stream http request and response body.
// can set timeout when body stream is unavailable, which didn't reach EOF yet.
//
// Copyright (C) 2020-present Himawari Tachibana <fieliapm@gmail.com>
//
// This file is part of stream-http-go
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package request_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fieliapm/stream-http-go/pkg/request"
)

const (
	testBodySize       int           = 512 * 1024 * 1024
	testSucceedTimeout time.Duration = 1000 * time.Millisecond
	testFailTimeout    time.Duration = 50 * time.Millisecond
	testNoTimeout      time.Duration = 0

	testReadTimeout     time.Duration = 1000 * time.Millisecond
	testWriteTimeout    time.Duration = 1000 * time.Millisecond
	testResponseDelay   time.Duration = 200 * time.Millisecond
	testWaitServerDelay time.Duration = 250 * time.Millisecond
	testPort            int           = 65535

	testGCSleep time.Duration = 1500 * time.Millisecond
)

var (
	reqBytes []byte
)

func getAuthorization(req *http.Request) (authType string, authCredentials string) {
	authorizationHeaderValue := req.Header.Get("Authorization")

	authorizationData := strings.SplitN(authorizationHeaderValue, " ", 2)
	if len(authorizationData) != 2 {
		return
	}

	authType = authorizationData[0]
	authCredentials = authorizationData[1]
	return
}

func handleFunc(w http.ResponseWriter, req *http.Request) {
	defer func() {
		fmt.Fprintln(os.Stderr, "[server] request complete")
	}()

	fmt.Fprintln(os.Stderr, "[server] receiving request")
	var respBody bytes.Buffer

	n, err := request.TimeoutCopy(&respBody, req.Body, testReadTimeout, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[server] receiving request failed at byte %d\n", n)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("Read Error at byte %d\n", n)))
		return
	}
	fmt.Fprintf(os.Stderr, "[server] received request with length %d\n", respBody.Len())
	if respBody.Len() <= 0 {
		reqBody := bytes.NewReader(reqBytes)
		n, err = io.Copy(&respBody, reqBody)
	}

	fmt.Fprintln(os.Stderr, "[server] handling request")
	runtime.GC()

	authType, authCredentials := getAuthorization(req)
	if authType == "Bearer" {
		if authCredentials == "qawsedrftgyhujikolp" {
			withContentLength, err := strconv.Atoi(req.URL.Query().Get("with_content_length"))
			if err != nil {
				fmt.Fprintln(os.Stderr, "[server] bad request")
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte("Bad Request\n"))
				return
			}

			fmt.Fprintf(os.Stderr, "[server] sending response with length %d\n", respBody.Len())
			w.Header().Set("Content-Type", "application/octet-stream")
			if withContentLength != 0 {
				w.Header().Set("Content-Length", strconv.Itoa(respBody.Len()))
			}
			w.WriteHeader(http.StatusOK)
			n, err = request.TimeoutCopy(w, &respBody, testWriteTimeout, true)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[server] sending response failed at byte %d\n", n)
			}
			time.Sleep(testResponseDelay)
			return
		}
	}

	fmt.Fprintln(os.Stderr, "[server] authorization failed")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte("Unauthorized\n"))
	return
}

func runServer() *http.Server {
	fmt.Fprintln(os.Stderr, "[server] server starting")

	serveMux := http.NewServeMux()
	serveMux.HandleFunc("/ping-pong", handleFunc)
	server := &http.Server{Addr: fmt.Sprintf(":%d", testPort), Handler: serveMux}

	go func() {
		fmt.Fprintln(os.Stderr, "[server] server started")
		server.ListenAndServe()
		fmt.Fprintln(os.Stderr, "[server] server shutted down")
	}()

	// wait for server started
	time.Sleep(testWaitServerDelay)

	return server
}

func TestMain(m *testing.M) {
	fmt.Fprintln(os.Stderr, "[common] generating binary")
	reqBytes = make([]byte, testBodySize)
	rand.Read(reqBytes)

	server := runServer()

	exitCode := m.Run()

	fmt.Fprintln(os.Stderr, "[server] server shutting down")
	server.Shutdown(context.Background())

	// wait for server shutted down
	time.Sleep(testWaitServerDelay)

	os.Exit(exitCode)
}

func testRequest(t *testing.T, timeout time.Duration, method string, withContentLength bool, expectError bool, ignoreResponseBody bool) {
	defer func() {
		fmt.Fprintln(os.Stderr, "[client] test complete")
	}()

	var reqBody io.Reader
	switch method {
	case http.MethodHead:
	case http.MethodGet:
	case http.MethodDelete:
	default:
		reqBody = bytes.NewReader(reqBytes)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := http.DefaultClient

	fmt.Fprintln(os.Stderr, "[client] begin request")

	urlString := fmt.Sprintf("http://127.0.0.1:%d/ping-pong", testPort)

	var opts []request.Option
	requestMod := request.RequestMod(func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer qawsedrftgyhujikolp")

		query := req.URL.Query()
		var v string
		if withContentLength {
			v = "1"
		} else {
			v = "0"
		}
		query.Add("with_content_length", v)
		req.URL.RawQuery = query.Encode()
	})
	opts = append(opts, requestMod)
	if timeout > time.Duration(0) {
		opts = append(opts, request.Timeout(timeout, cancel))
	}

	var respBody bytes.Buffer
	var respBodyP io.Writer
	if !ignoreResponseBody {
		respBodyP = &respBody
	}

	var resp *http.Response
	var err error
	resp, err = request.DoRequest(ctx, client, method, urlString, reqBody, respBodyP, opts...)

	fmt.Fprintln(os.Stderr, "[client] end request")

	if expectError {
		require.Error(t, err)
	} else {
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		var cmpResult int
		if !ignoreResponseBody {
			if method == http.MethodHead {
				cmpResult = bytes.Compare([]byte{}, respBody.Bytes())
			} else {
				cmpResult = bytes.Compare(reqBytes, respBody.Bytes())
			}
		}
		assert.Equal(t, 0, cmpResult)
	}
}

func cleanUp() {
	time.Sleep(testGCSleep)
	fmt.Fprintln(os.Stderr, "[client] GC start")
	runtime.GC()
	fmt.Fprintln(os.Stderr, "[client] GC end")
	time.Sleep(testGCSleep)
}

func TestHeadWithContentLength(t *testing.T) {
	testRequest(t, testSucceedTimeout, http.MethodHead, true, false, false)
	cleanUp()
}

func TestGetWithContentLength(t *testing.T) {
	testRequest(t, testSucceedTimeout, http.MethodGet, true, false, false)
	cleanUp()
}

func TestGet(t *testing.T) {
	testRequest(t, testSucceedTimeout, http.MethodGet, false, false, false)
	cleanUp()
}

func TestGetAndIgnoreResponseBody(t *testing.T) {
	testRequest(t, testSucceedTimeout, http.MethodGet, false, false, true)
	cleanUp()
}

func TestGetWithFailTimeout(t *testing.T) {
	testRequest(t, testFailTimeout, http.MethodGet, false, true, false)
	cleanUp()
}

func TestDelete(t *testing.T) {
	testRequest(t, testSucceedTimeout, http.MethodDelete, false, false, false)
	cleanUp()
}

func TestPostWithContentLength(t *testing.T) {
	testRequest(t, testSucceedTimeout, http.MethodPost, true, false, false)
	cleanUp()
}

func TestPost(t *testing.T) {
	testRequest(t, testSucceedTimeout, http.MethodPost, false, false, false)
	cleanUp()
}

func TestPostAndIgnoreResponseBody(t *testing.T) {
	testRequest(t, testSucceedTimeout, http.MethodPost, false, false, true)
	cleanUp()
}

func TestPostWithFailTimeout(t *testing.T) {
	testRequest(t, testFailTimeout, http.MethodPost, false, true, false)
	cleanUp()
}

func TestPostWithoutTimeout(t *testing.T) {
	testRequest(t, testNoTimeout, http.MethodPost, false, false, false)
	cleanUp()
}
