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

package request

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	chunkSize int = 0x1 << 16
)

type TimeoutReader struct {
	reader  io.Reader
	timer   *time.Timer
	timeout time.Duration
	i       time.Time
}

func NewTimeoutReader(reader io.Reader, timer *time.Timer, timeout time.Duration) *TimeoutReader {
	return &TimeoutReader{
		reader:  reader,
		timer:   timer,
		timeout: timeout,
	}
}

func (timeoutReader *TimeoutReader) Read(p []byte) (nRead int, err error) {
	timeoutReader.timer.Stop()
	nRead, err = timeoutReader.reader.Read(p)
	timeoutReader.timer.Reset(timeoutReader.timeout)
	return
}

func TimeoutCopy(writer io.Writer, reader io.Reader, timer *time.Timer, timeout time.Duration) (written int64, err error) {
	readBuf := make([]byte, chunkSize)

	for {
		timer.Reset(timeout)
		nr, er := reader.Read(readBuf)
		timer.Stop()
		if nr > 0 {
			nw, ew := writer.Write(readBuf[0:nr])
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

// Option

type config struct {
	header       http.Header
	query        url.Values
	requestBody  io.Reader
	responseBody io.Writer
	timeout      time.Duration
	timeoutFunc  func()
}

type Option func(conf *config)

func Header(header http.Header) Option {
	return func(conf *config) {
		conf.header = header
	}
}

func Query(query url.Values) Option {
	return func(conf *config) {
		conf.query = query
	}
}

func RequestBody(requestBody io.Reader) Option {
	return func(conf *config) {
		conf.requestBody = requestBody
	}
}

func ResponseBody(responseBody io.Writer) Option {
	return func(conf *config) {
		conf.responseBody = responseBody
	}
}

func Timeout(timeout time.Duration, timeoutFunc func()) Option {
	return func(conf *config) {
		conf.timeout = timeout
		conf.timeoutFunc = timeoutFunc
	}
}

// function

func DoRequest(ctx context.Context, client *http.Client, method string, url string, opts ...Option) (resp *http.Response, err error) {
	conf := config{}
	for _, opt := range opts {
		opt(&conf)
	}

	var timer *time.Timer
	if conf.timeout > time.Duration(0) {
		timer = time.AfterFunc(conf.timeout, conf.timeoutFunc)
	}

	var requestBody io.Reader
	if conf.requestBody != nil {
		if timer != nil {
			requestBody = NewTimeoutReader(conf.requestBody, timer, conf.timeout)
		} else {
			requestBody = conf.requestBody
		}
	}

	var req *http.Request
	req, err = http.NewRequest(method, url, requestBody)
	if err != nil {
		return
	}

	req = req.WithContext(ctx)

	if conf.header != nil {
		header := req.Header
		for key, values := range conf.header {
			for _, value := range values {
				header.Add(key, value)
			}
		}
	}

	if conf.query != nil {
		query := req.URL.Query()
		for key, values := range conf.query {
			for _, value := range values {
				query.Add(key, value)
			}
		}
		req.URL.RawQuery = query.Encode()
	}

	resp, err = client.Do(req)
	if timer != nil {
		timer.Stop()
	}
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if conf.responseBody != nil {
		if timer != nil {
			_, err = TimeoutCopy(conf.responseBody, resp.Body, timer, conf.timeout)
		} else {
			_, err = io.Copy(conf.responseBody, resp.Body)
		}
	}

	return
}
