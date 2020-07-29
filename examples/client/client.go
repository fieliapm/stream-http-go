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

package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/fieliapm/stream-http-go/pkg/request"
)

func playRequest() error {
	ctx := context.Background()

	client := http.DefaultClient

	requestMod := request.RequestMod(func(req *http.Request) {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Authorization", "Bearer qawsedrftgyhujikolp")

		query := req.URL.Query()
		query.Add("param", "pvalue")
		req.URL.RawQuery = query.Encode()
	})
	requestTimeout := request.Timeout(500 * time.Millisecond)

	reqBody := bytes.NewBuffer([]byte("form=fvalue"))
	var respBody bytes.Buffer

	resp, err := request.DoRequest(ctx, client, http.MethodPost, "http://httpbin.org/anything", reqBody, &respBody, requestMod, requestTimeout)
	//resp, err := request.DoRequest(ctx, client, http.MethodGet, "http://httpbin.org/status/401", nil, &respBody, requestMod, requestTimeout)
	if err != nil {
		return err
	}

	fmt.Printf("get response with status code: %d\n%s\n", resp.StatusCode, respBody.String())

	return nil
}

func main() {
	err := playRequest()
	if err != nil {
		panic(err)
	}
}
