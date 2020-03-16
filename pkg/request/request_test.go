// stream-http-go - stream http request and response body.
// can set timeout when body stream is unavailable, which didn't reach EOF yet.
// Copyright (C) 2020-present Himawari Tachibana <fieliapm@gmail.com>
//
// This file is part of stream-http-go
//
// stream-http-go is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fieliapm/stream-http-go/pkg/request"
)

const (
	testPort           int           = 65535
	testSucceedTimeout time.Duration = 500 * time.Millisecond
	testFailTimeout    time.Duration = 50 * time.Millisecond
	testNoTimeout      time.Duration = 0
	statusCode         int           = http.StatusOK
	bodySize           int           = 512 * 1024 * 1024
)

var (
	reqBytes []byte
)

func createHandleFunc(hasContentLength bool) func(w http.ResponseWriter, req *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		defer func() {
			fmt.Fprintln(os.Stderr, "[server] request complete")
			runtime.GC()
		}()

		fmt.Fprintln(os.Stderr, "[server] receiving request")
		var reqBody bytes.Buffer
		io.Copy(&reqBody, req.Body)

		fmt.Fprintf(os.Stderr, "[server] sending response with length %d\n", reqBody.Len())
		w.Header().Set("Content-Type", "application/octet-stream")
		if hasContentLength {
			w.Header().Set("Content-Length", strconv.Itoa(reqBody.Len()))
		}
		w.WriteHeader(statusCode)
		io.Copy(w, &reqBody)
	}
}

func runServer() *http.Server {
	fmt.Fprintln(os.Stderr, "[server] server starting")

	serveMux := http.NewServeMux()
	serveMux.HandleFunc("/ping-pong/without-content-length", createHandleFunc(false))
	serveMux.HandleFunc("/ping-pong/with-content-length", createHandleFunc(true))
	server := &http.Server{Addr: fmt.Sprintf(":%d", testPort), Handler: serveMux}

	go func() {
		fmt.Fprintln(os.Stderr, "[server] server started")
		server.ListenAndServe()
		fmt.Fprintln(os.Stderr, "[server] server shutted down")
	}()

	// wait for server started
	time.Sleep(250 * time.Millisecond)

	return server
}

func TestMain(m *testing.M) {
	server := runServer()

	fmt.Fprintln(os.Stderr, "[client] generating binary")
	reqBytes = make([]byte, bodySize)
	rand.Read(reqBytes)

	exitCode := m.Run()

	fmt.Fprintln(os.Stderr, "[server] server shutting down")
	server.Shutdown(context.Background())

	// wait for server shutted down
	time.Sleep(250 * time.Millisecond)

	os.Exit(exitCode)
}

func testPost(t *testing.T, timeout time.Duration, withContentLength bool, expectError bool) {
	reqBody := bytes.NewReader(reqBytes)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := http.DefaultClient

	fmt.Fprintln(os.Stderr, "[client] begin request")

	var s string
	if withContentLength {
		s = "with"
	} else {
		s = "without"
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/ping-pong/%s-content-length", testPort, s)

	var respBody bytes.Buffer
	var resp *http.Response
	var err error
	if timeout > time.Duration(0) {
		resp, err = request.DoRequest(ctx, client, http.MethodPost, url,
			request.RequestBody(reqBody), request.ResponseBody(&respBody), request.Timeout(timeout, cancel))
	} else {
		resp, err = request.DoRequest(ctx, client, http.MethodPost, url,
			request.RequestBody(reqBody), request.ResponseBody(&respBody))
	}

	fmt.Fprintln(os.Stderr, "[client] end request")

	if expectError {
		require.Error(t, err)
	} else {
		require.NoError(t, err)
		assert.Equal(t, statusCode, resp.StatusCode)
		cmpResult := bytes.Compare(reqBytes, respBody.Bytes())
		assert.Equal(t, 0, cmpResult)
	}
}

func TestPostWithContentLength(t *testing.T) {
	testPost(t, testSucceedTimeout, true, false)
}

func TestPostWithoutContentLength(t *testing.T) {
	testPost(t, testSucceedTimeout, false, false)
}

func TestPostWithFailTimeout(t *testing.T) {
	testPost(t, testFailTimeout, true, true)
}

func TestPostWithoutTimeout(t *testing.T) {
	testPost(t, testNoTimeout, true, false)
}
