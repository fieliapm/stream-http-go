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
	"strconv"
	"time"
)

const (
	chunkSize int = 0x1 << 16
)

// timeout IO

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

type RequestModFunc func(*http.Request)

type config struct {
	requestModFunc RequestModFunc
	timeout        time.Duration
	timeoutFunc    func()
}

type Option func(conf *config)

func RequestMod(requestModFunc RequestModFunc) Option {
	return func(conf *config) {
		conf.requestModFunc = requestModFunc
	}
}

func Timeout(timeout time.Duration, timeoutFunc func()) Option {
	return func(conf *config) {
		conf.timeout = timeout
		conf.timeoutFunc = timeoutFunc
	}
}

// function

func DoRequest(ctx context.Context, client *http.Client, method string, urlString string, requestBody io.Reader, responseBody io.Writer, opts ...Option) (resp *http.Response, err error) {
	conf := config{}
	for _, opt := range opts {
		opt(&conf)
	}

	var timer *time.Timer
	if conf.timeout > time.Duration(0) {
		timer = time.AfterFunc(conf.timeout, conf.timeoutFunc)
	}

	if requestBody != nil {
		if timer != nil {
			requestBody = NewTimeoutReader(requestBody, timer, conf.timeout)
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
			nWritten, err = TimeoutCopy(responseBody, resp.Body, timer, conf.timeout)
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
