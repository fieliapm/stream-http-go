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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fieliapm/stream-http-go/pkg/request"
)

const (
	testPort   int           = 8090
	timeout    time.Duration = 500 * time.Millisecond
	statusCode int           = 200
	bodySize   int           = 512 * 1024 * 1024
)

func handleFunc(w http.ResponseWriter, req *http.Request) {
	fmt.Fprintln(os.Stderr, "[server] receiving request")
	var reqBody bytes.Buffer
	io.Copy(&reqBody, req.Body)

	fmt.Fprintln(os.Stderr, "[server] sending response")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(statusCode)
	io.Copy(w, &reqBody)

	fmt.Fprintln(os.Stderr, "[server] request complete")
}

func runServer() *http.Server {
	fmt.Fprintln(os.Stderr, "[server] server starting")

	serveMux := http.NewServeMux()
	serveMux.HandleFunc("/", handleFunc)
	server := &http.Server{Addr: fmt.Sprintf(":%d", testPort), Handler: serveMux}

	go func() {
		server.ListenAndServe()
	}()

	// wait for server started
	time.Sleep(500 * time.Millisecond)

	fmt.Fprintln(os.Stderr, "[server] server started")
	return server
}

func TestRequest(t *testing.T) {
	server := runServer()
	defer func() {
		fmt.Fprintln(os.Stderr, "[server] server shutdown")
		server.Shutdown(context.Background())
	}()

	fmt.Fprintln(os.Stderr, "[client] generating binary")
	reqBytes := make([]byte, bodySize)
	rand.Read(reqBytes)
	reqBody := bytes.NewReader(reqBytes)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := http.DefaultClient

	fmt.Fprintln(os.Stderr, "[client] begin request")
	var respBody bytes.Buffer
	resp, err := request.DoRequest(ctx, client, http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d/", testPort),
		request.RequestBody(reqBody), request.ResponseBody(&respBody), request.Timeout(timeout, cancel))
	fmt.Fprintln(os.Stderr, "[client] end request")

	require.NoError(t, err)
	assert.Equal(t, statusCode, resp.StatusCode)
	cmpResult := bytes.Compare(reqBytes, respBody.Bytes())
	assert.Equal(t, 0, cmpResult)
}
