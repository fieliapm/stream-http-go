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

package request

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"
)

const (
	chunkSize int = 0x1 << 16
)

var (
	ErrorCopyTimeout = errors.New("copy timeout")
)

// timeout IO

type timeoutReader struct {
	reader  io.Reader
	timer   *time.Timer
	timeout time.Duration
}

func newTimeoutReader(reader io.Reader, timer *time.Timer, timeout time.Duration) *timeoutReader {
	return &timeoutReader{
		reader:  reader,
		timer:   timer,
		timeout: timeout,
	}
}

func (tr *timeoutReader) Read(p []byte) (nRead int, err error) {
	tr.timer.Stop()
	nRead, err = tr.reader.Read(p)
	tr.timer.Reset(tr.timeout)
	return
}

func internalTimeoutCopy(dst io.Writer, src io.Reader, timer *time.Timer, timeout time.Duration, writeTimeout bool) (written int64, err error) {
	readBuf := make([]byte, chunkSize)

	for {
		if !writeTimeout {
			timer.Reset(timeout)
		}
		nr, er := src.Read(readBuf)
		if !writeTimeout {
			timer.Stop()
		}
		if nr > 0 {
			if writeTimeout {
				timer.Reset(timeout)
			}
			nw, ew := dst.Write(readBuf[0:nr])
			if writeTimeout {
				timer.Stop()
			}
			if nw > 0 {
				written += int64(nw)
			}
			if ew != nil {
				err = ew
				break
			}
			if nr != nw {
				err = io.ErrShortWrite
				break
			}
		}
		if er != nil {
			if er != io.EOF {
				err = er
			}
			break
		}
	}

	return
}

// option

type RequestModFunc func(*http.Request)

type config struct {
	requestModFunc RequestModFunc
	timeout        time.Duration
}

type Option func(conf *config)

func RequestMod(requestModFunc RequestModFunc) Option {
	return func(conf *config) {
		conf.requestModFunc = requestModFunc
	}
}

func Timeout(timeout time.Duration) Option {
	return func(conf *config) {
		conf.timeout = timeout
	}
}

// function

// DoRequest sends an HTTP request and returns an HTTP response, following policy (such as redirects, cookies, auth) as configured on the client.
//
// Its behaviour is same as Client.Do in standard library package net/http, except it writes response to provided responseBody before returned, and it will cancel request when sending/receiving a chunk encounters timeout.
//
// There are some options:
// RequestMod(func(*http.Request)) sets request headers and query string parameters.
// Timeout(time.Duration) sets timeout.
//
// An error is returned if caused by client policy (such as CheckRedirect), or failure to speak HTTP (such as a network connectivity problem). A non-2xx status code doesn't cause an error.
//
// If the returned error is nil, the Response will contain a non-nil Body which the user is expected to close. If the Body is not both read to EOF and closed, the Client's underlying RoundTripper (typically Transport) may not be able to re-use a persistent TCP connection to the server for a subsequent "keep-alive" request.
//
// The request Body, if non-nil, will be closed by the underlying Transport, even on errors.
//
// On error, any Response can be ignored. A non-nil Response with a non-nil error only occurs when CheckRedirect fails, and even then the returned Response.Body is already closed.
func DoRequest(ctx context.Context, client *http.Client, method string, urlString string, requestBody io.Reader, responseBody io.Writer, opts ...Option) (resp *http.Response, err error) {
	conf := config{}
	for _, opt := range opts {
		opt(&conf)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var timer *time.Timer
	if conf.timeout > time.Duration(0) {
		timer = time.AfterFunc(conf.timeout, cancel)
	}

	if requestBody != nil {
		if timer != nil {
			requestBody = newTimeoutReader(requestBody, timer, conf.timeout)
		}
	}

	var req *http.Request
	req, err = http.NewRequest(method, urlString, requestBody)
	if err != nil {
		return
	}
	req = req.WithContext(ctx)

	if conf.requestModFunc != nil {
		conf.requestModFunc(req)
	}

	resp, err = client.Do(req)
	if timer != nil {
		timer.Stop()
	}
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if responseBody != nil {
		var nWritten int64
		if timer != nil {
			nWritten, err = internalTimeoutCopy(responseBody, resp.Body, timer, conf.timeout, false)
		} else {
			nWritten, err = io.Copy(responseBody, resp.Body)
		}
		if err != nil {
			return
		}

		if req.Method != http.MethodHead {
			contentLengthString := resp.Header.Get("Content-Length")
			if len(contentLengthString) > 0 {
				var contentLength int64
				contentLength, err = strconv.ParseInt(contentLengthString, 10, 64)
				if err != nil {
					return
				}

				if contentLength != nWritten {
					err = io.ErrUnexpectedEOF
					return
				}
			}
		}
	}

	return
}

// TimeoutCopy copies from src to dst until EOF is reached on src, an error occurs, or reading/writing a chunk encounters provided timeout.
//
// While writeTimeout == false, it sets timeout for reading a chunk.
// While writeTimeout == true, it sets timeout for writing a chunk.
//
// If it encounters timeout, it will return zero bytes copied and error ErrorCopyTimeout.
// If it does not encounter any timeout, it returns the number of bytes copied and the first error encountered while copying, if any.
//
// A successful Copy returns err == nil, not err == EOF.
// Because Copy is defined to read from src until EOF, it does not treat an EOF from Read as an error to be reported.
func TimeoutCopy(dst io.Writer, src io.Reader, timeout time.Duration, writeTimeout bool) (written int64, err error) {
	type copyResult struct {
		written int64
		err     error
	}

	doneChan := make(chan struct{})
	defer close(doneChan)

	panicChan := make(chan interface{})
	copyResultChan := make(chan copyResult)

	timer := time.NewTimer(timeout)
	if !timer.Stop() {
		<-timer.C
	}

	go func() {
		defer func() {
			p := recover()
			if p != nil {
				select {
				case panicChan <- p:
				case <-doneChan:
				}
			}
		}()

		nw, er := internalTimeoutCopy(dst, src, timer, timeout, writeTimeout)
		result := copyResult{
			written: nw,
			err:     er,
		}
		select {
		case copyResultChan <- result:
		case <-doneChan:
		}
	}()

	select {
	case p := <-panicChan:
		panic(p)
	case result := <-copyResultChan:
		written = result.written
		err = result.err
	case <-timer.C:
		written = 0
		err = ErrorCopyTimeout
	}

	return
}
