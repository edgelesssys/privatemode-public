// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: GPL-3.0-only

package forwarder

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWithSelectJSONRequestMutation(t *testing.T) {
	testCases := map[string]struct {
		mutator          stubMutator
		fields           FieldSelector
		requestBody      string
		expectedResponse string
		wantErr          bool
	}{
		"single field simple string": {
			mutator: stubMutator{
				mutateResponse: `"encryptedText"`,
			},
			requestBody:      `{"field1": "plainText"}`,
			fields:           FieldSelector{{"field1"}},
			expectedResponse: `{"field1": "encryptedText"}`,
		},
		"single field nested JSON": {
			mutator: stubMutator{
				mutateResponse: `{"nestedField": "plainText"}`,
			},
			requestBody:      `{"field1": "encryptedText"}`,
			fields:           FieldSelector{{"field1"}},
			expectedResponse: `{"field1": {"nestedField": "plainText"}}`,
		},
		"multiple fields": {
			mutator: stubMutator{
				mutateResponse: `"plainText"`,
			},
			requestBody:      `{"field1": "encryptedText1", "field2": "encryptedText2"}`,
			fields:           FieldSelector{{"field1"}, {"field2"}},
			expectedResponse: `{"field1": "plainText", "field2": "plainText"}`,
		},
		"mutate error": {
			mutator: stubMutator{
				mutateErr: assert.AnError,
			},
			requestBody: `{"field1": "encryptedText"}`,
			fields:      FieldSelector{{"field1"}},
			wantErr:     true,
		},
		"missing fields are skipped": {
			mutator: stubMutator{
				mutateResponse: `"plainText"`,
			},
			requestBody:      `{"field1": "encryptedText1"}`,
			fields:           FieldSelector{{"field1"}, {"field2"}},
			expectedResponse: `{"field1": "plainText"}`,
		},
		"not selected fields are skipped": {
			mutator: stubMutator{
				mutateResponse: `"plainText"`,
			},
			requestBody:      `{"field1": "encryptedText1", "field2": "encryptedText2"}`,
			fields:           FieldSelector{{"field2"}},
			expectedResponse: `{"field1": "encryptedText1", "field2": "plainText"}`,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			mutate := WithSelectJSONRequestMutation(tc.mutator.mutate, tc.fields, slog.Default())
			request := &http.Request{
				Body: io.NopCloser(bytes.NewBufferString(tc.requestBody)),
			}

			err := mutate(request)
			if tc.wantErr {
				assert.Error(err)
				return
			}

			assert.NoError(err)
			body, err := io.ReadAll(request.Body)
			assert.NoError(err)
			assert.Equal(tc.expectedResponse, string(body))
		})
	}
}

func TestWithFullRequestMutation(t *testing.T) {
	testCases := map[string]struct {
		mutator          stubMutator
		requestBody      string
		expectedResponse string
		contentType      string
		wantErr          bool
	}{
		"plain text mutation": {
			mutator: stubMutator{
				mutateResponse: "mutated text",
			},
			requestBody:      "original text",
			expectedResponse: "mutated text",
			contentType:      "text/plain",
		},
		"JSON body mutation": {
			mutator: stubMutator{
				mutateResponse: `mutated`,
			},
			requestBody:      `{"field": "original"}`,
			expectedResponse: `mutated`,
			contentType:      "application/json",
		},
		"empty body": {
			mutator: stubMutator{
				mutateResponse: "",
			},
			requestBody:      "",
			expectedResponse: "",
			contentType:      "text/plain",
		},
		"mutation error": {
			mutator: stubMutator{
				mutateErr: errors.New("mutation failed"),
			},
			requestBody: "some data",
			wantErr:     true,
			contentType: "text/plain",
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			mutate := WithFullRequestMutation(tc.mutator.mutate, slog.Default())

			request := &http.Request{
				Header: make(http.Header),
				Body:   io.NopCloser(bytes.NewBufferString(tc.requestBody)),
			}
			request.Header.Set("Content-Type", tc.contentType)

			err := mutate(request)
			if tc.wantErr {
				assert.Error(err)
				return
			}

			assert.NoError(err)
			body, err := io.ReadAll(request.Body)
			assert.NoError(err)
			assert.Equal(tc.expectedResponse, string(body))
		})
	}
}

func TestWithFullJSONRequestMutation(t *testing.T) {
	testCases := map[string]struct {
		mutator          stubMutator
		skipFields       FieldSelector
		requestBody      string
		expectedResponse string
		wantErr          bool
	}{
		"empty body": {
			mutator: stubMutator{
				mutateResponse: `"encryptedText"`,
			},
			requestBody:      "",
			expectedResponse: "",
		},
		"single field simple string": {
			mutator: stubMutator{
				mutateResponse: `"encryptedText"`,
			},
			requestBody:      `{"field1": "plainText"}`,
			expectedResponse: `{"field1": "encryptedText"}`,
		},
		"single field nested JSON": {
			mutator: stubMutator{
				mutateResponse: `{"nestedField": "plainText"}`,
			},
			requestBody:      `{"field1": "encryptedText"}`,
			expectedResponse: `{"field1": {"nestedField": "plainText"}}`,
		},
		"multiple fields": {
			mutator: stubMutator{
				mutateResponse: `"encryptedText"`,
			},
			requestBody:      `{"field1": "plainText", "field2": "plainText"}`,
			expectedResponse: `{"field1": "encryptedText", "field2": "encryptedText"}`,
		},
		"mutate error": {
			mutator: stubMutator{
				mutateErr: assert.AnError,
			},
			requestBody: `{"field1": "encryptedText"}`,
			wantErr:     true,
		},
		"fields can be skipped": {
			mutator: stubMutator{
				mutateResponse: `"encryptedText"`,
			},
			requestBody:      `{"field1": "plainText", "field2": "plainText"}`,
			skipFields:       FieldSelector{{"field1"}},
			expectedResponse: `{"field1": "plainText", "field2": "encryptedText"}`,
		},
		"skip fields may be missing from body": {
			mutator: stubMutator{
				mutateResponse: `"encryptedText"`,
			},
			requestBody:      `{"field1": "plainText", "field2": "plainText"}`,
			skipFields:       FieldSelector{{"field3"}},
			expectedResponse: `{"field1": "encryptedText", "field2": "encryptedText"}`,
		},
		"nested fields can be skipped": {
			mutator: stubMutator{
				mutateResponse: `"encryptedText"`,
			},
			requestBody:      `{"field1": {"nestedField1": "plainText", "nestedField2": "plainText"}, "field2": "plainText"}`,
			skipFields:       FieldSelector{{"field1", "nestedField1"}},
			expectedResponse: `{"field1": {"nestedField1": "plainText", "nestedField2": "encryptedText"}, "field2": "encryptedText"}`,
		},
		"invalid JSON data throws an error": {
			mutator: stubMutator{
				mutateResponse: `"encryptedText"`,
			},
			requestBody: `{"field1": "encryptedText"`,
			wantErr:     true,
		},
		"fields with dots in name can be skipped": {
			mutator: stubMutator{
				mutateResponse: `"encryptedText"`,
			},
			requestBody:      `{"field1": {"nested.field": "plainText"}, "field2": "plainText", "field2.3": "plainText"}`,
			skipFields:       FieldSelector{{"field1", "nested\\.field"}},
			expectedResponse: `{"field1": {"nested.field": "plainText"}, "field2": "encryptedText", "field2.3": "encryptedText"}`,
		},
		"multiple fields can be skipped": {
			mutator: stubMutator{
				mutateResponse: `"encryptedText"`,
			},
			requestBody:      `{"field1": "plainText", "field2": "plainText", "field3": "plainText"}`,
			skipFields:       FieldSelector{{"field1"}, {"field2"}},
			expectedResponse: `{"field1": "plainText", "field2": "plainText", "field3": "encryptedText"}`,
		},
		"nested fields are encrypted as single fields": {
			mutator: stubMutator{
				mutateResponse: `"encryptedText"`,
			},
			requestBody:      `{"field1": {"nestedField1": "plainText", "nestedField2": "plainText"}, "field2": "plainText"}`,
			expectedResponse: `{"field1": "encryptedText", "field2": "encryptedText"}`,
		},
		"nested array fields can be skipped": {
			mutator: stubMutator{
				mutateResponse: `"encryptedText"`,
			},
			requestBody:      `{"field1": [{"key1": "plainText", "key2": "plainText"}, {"key1": "plainText", "key2": "plaintText"}], "field2": "plainText"}`,
			skipFields:       FieldSelector{{"field1", "#", "key1"}},
			expectedResponse: `{"field1": [{"key1": "plainText", "key2": "encryptedText"}, {"key1": "plainText", "key2": "encryptedText"}], "field2": "encryptedText"}`,
		},
		"specific nested array fields can be skipped": {
			mutator: stubMutator{
				mutateResponse: `"encryptedText"`,
			},
			requestBody:      `{"field1": [{"key1": "plainText", "key2": "plainText"}, {"key1": "plainText", "key2": "plaintText"}], "field2": "plainText"}`,
			skipFields:       FieldSelector{{"field1", "0", "key1"}},
			expectedResponse: `{"field1": [{"key1": "plainText", "key2": "encryptedText"}, "encryptedText"], "field2": "encryptedText"}`,
		},
		"array fields can be skipped": {
			mutator: stubMutator{
				mutateResponse: `"encryptedText"`,
			},
			requestBody:      `{"field1": ["plainText", "plainText"], "field2": "plainText"}`,
			skipFields:       FieldSelector{{"field1", "#"}},
			expectedResponse: `{"field1": ["plainText", "plainText"], "field2": "encryptedText"}`,
		},
		"specific array fields can be skipped": {
			mutator: stubMutator{
				mutateResponse: `"encryptedText"`,
			},
			requestBody:      `{"field1": ["plainText", "plainText"], "field2": "plainText"}`,
			skipFields:       FieldSelector{{"field1", "0"}},
			expectedResponse: `{"field1": ["plainText", "encryptedText"], "field2": "encryptedText"}`,
		},
		"specific missing nested array fields can be missing": {
			mutator: stubMutator{
				mutateResponse: `"encryptedText"`,
			},
			requestBody:      `{"field1": ["plainText", "plainText"], "field2": "plainText"}`,
			skipFields:       FieldSelector{{"field1", "0", "foo"}},
			expectedResponse: `{"field1": ["encryptedText", "encryptedText"], "field2": "encryptedText"}`,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			mutate := WithFullJSONRequestMutation(tc.mutator.mutate, tc.skipFields, slog.Default())
			request := &http.Request{
				Body: io.NopCloser(bytes.NewBufferString(tc.requestBody)),
			}

			err := mutate(request)
			if tc.wantErr {
				assert.Error(err)
				return
			}

			assert.NoError(err)
			body, err := io.ReadAll(request.Body)
			assert.NoError(err)
			assert.Equal(tc.expectedResponse, string(body))
		})
	}
}

func TestNewlinePreservationForStreamEventData(t *testing.T) {
	assert := assert.New(t)

	// http streaming separates events with double newlines
	dataFmt := `data: {"field1": "%s"}` + "\n\n"

	data := fmt.Sprintf(dataFmt, "plainText")
	b := bytes.NewBufferString(data)
	readCloser := io.NopCloser(b)

	responseMutator := WithSelectJSONResponseMutation(
		stubMutator{
			mutateResponse: `"encryptedText"`,
		}.mutate,
		FieldSelector{{"field1"}},
	)
	sut := responseMutator.Reader(readCloser)

	res := &bytes.Buffer{}
	_, err := io.Copy(res, sut)
	assert.NoError(err)
	expectedResponse := fmt.Sprintf(dataFmt, "encryptedText")
	assert.Equal(expectedResponse, res.String())
}

func TestWithSelectResponseMutation(t *testing.T) {
	testCases := map[string]struct {
		mutator          stubMutator
		responseBody     string
		fields           FieldSelector
		expectedResponse string
		wantErr          bool
	}{
		"single field mutation": {
			mutator: stubMutator{
				mutateResponse: `"encryptedText"`,
			},
			responseBody:     `{"field1": "plainText"}`,
			fields:           FieldSelector{{"field1"}},
			expectedResponse: `{"field1": "encryptedText"}`,
		},
		"multiple fields": {
			mutator: stubMutator{
				mutateResponse: `"encryptedText"`,
			},
			responseBody:     `{"field1": "plainText1", "field2": "plainText2"}`,
			fields:           FieldSelector{{"field1"}, {"field2"}},
			expectedResponse: `{"field1": "encryptedText", "field2": "encryptedText"}`,
		},
		"missing fields are skipped": {
			mutator: stubMutator{
				mutateResponse: `"encryptedText"`,
			},
			responseBody:     `{"field1": "plainText"}`,
			fields:           FieldSelector{{"field2"}},
			expectedResponse: `{"field1": "plainText"}`,
		},
		"not selected fields are skipped": {
			mutator: stubMutator{
				mutateResponse: `"encryptedText"`,
			},
			responseBody:     `{"field1": "plainText", "field2": "plainText2"}`,
			fields:           FieldSelector{{"field2"}},
			expectedResponse: `{"field1": "plainText", "field2": "encryptedText"}`,
		},
		"mutate from nested JSON": {
			mutator: stubMutator{
				mutateResponse: `"encryptedText"`,
			},
			responseBody:     `{"field1": {"nestedField": "plainText"}}`,
			fields:           FieldSelector{{"field1"}},
			expectedResponse: `{"field1": "encryptedText"}`,
		},
		"mutate to nested JSON": {
			mutator: stubMutator{
				mutateResponse: `{"nestedField": "plainText"}`,
			},
			responseBody:     `{"field1": "encryptedText"}`,
			fields:           FieldSelector{{"field1"}},
			expectedResponse: `{"field1": {"nestedField": "plainText"}}`,
		},
		"mutate error": {
			mutator: stubMutator{
				mutateErr: assert.AnError,
			},
			responseBody: `{"field1": "plainText"}`,
			fields:       FieldSelector{{"field1"}},
			wantErr:      true,
		},
		"mutate array fields": {
			mutator: stubMutator{
				mutateResponse: `"encryptedText"`,
			},
			responseBody:     `{"field1": [{"key": "plainText"}, {"key": "plainText"}]}`,
			fields:           FieldSelector{{"field1", "#", "key"}},
			expectedResponse: `{"field1": [{"key": "encryptedText"}, {"key": "encryptedText"}]}`,
		},
		"mutate deeply nested arrays": {
			mutator: stubMutator{
				mutateResponse: `"encryptedText"`,
			},
			responseBody:     `{"field1": [{"nested": [{"skip": "plainText", "mutate": "plainText1"}]}, {"nested": [{"skip": "plainText", "mutate": "plainText2"}]}]}`,
			fields:           FieldSelector{{"field1", "#", "nested", "#", "mutate"}},
			expectedResponse: `{"field1": [{"nested": [{"skip": "plainText", "mutate": "encryptedText"}]}, {"nested": [{"skip": "plainText", "mutate": "encryptedText"}]}]}`,
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			mutator := WithSelectJSONResponseMutation(tc.mutator.mutate, tc.fields)

			body, err := mutator.Mutate([]byte(tc.responseBody))
			if tc.wantErr {
				assert.Error(err)
				return
			}

			assert.NoError(err)
			assert.JSONEq(tc.expectedResponse, string(body))
		})
	}

	streamingCases := map[string]struct {
		mutator stubMutator
	}{
		"streaming mutation": {
			mutator: stubMutator{
				mutateResponse: `"encryptedText"`,
			},
		},
		"streaming large field mutation": {
			mutator: stubMutator{
				mutateResponse: fmt.Sprintf("\"%s\"", bytes.Repeat([]byte("encryptedText"), 10000)),
			},
		},
	}
	for name, tc := range streamingCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			mutator := WithSelectJSONResponseMutation(tc.mutator.mutate, FieldSelector{{"field1"}})

			msgChan := make(chan string)
			reader := &fakeReader{
				msgChan: msgChan,
			}

			messageParts := 10
			go func() {
				for i := range messageParts {
					msgChan <- fmt.Sprintf(`%d: {"field1": "plainText"}`, i) + "\n\n"
				}
				close(msgChan)
			}()

			response := &bytes.Buffer{}
			_, err := io.Copy(response, mutator.Reader(reader))
			assert.NoError(err)
			responseParts := strings.Split(strings.TrimRight(response.String(), "\n"), "\n\n") // trim the last newline, so that we don't get an empty final part
			assert.Len(responseParts, messageParts)
			for i, part := range responseParts {
				assert.Equal(fmt.Sprintf(`%d: {"field1": %s}`, i, tc.mutator.mutateResponse), part)
			}
		})
	}
}

func TestWithFullJSONResponseMutation(t *testing.T) {
	testCases := map[string]struct {
		mutator          stubMutator
		responseBody     string
		skipFields       FieldSelector
		expectedResponse string
		wantErr          bool
	}{
		"single field mutation": {
			mutator: stubMutator{
				mutateResponse: `"encryptedText"`,
			},
			responseBody:     `{"field1": "plainText"}`,
			expectedResponse: `{"field1": "encryptedText"}`,
		},
		"multiple fields": {
			mutator: stubMutator{
				mutateResponse: `"encryptedText"`,
			},
			responseBody:     `{"field1": "plainText1", "field2": "plainText2"}`,
			expectedResponse: `{"field1": "encryptedText", "field2": "encryptedText"}`,
		},
		"fields can be skipped": {
			mutator: stubMutator{
				mutateResponse: `"encryptedText"`,
			},
			responseBody:     `{"field1": "plainText"}`,
			skipFields:       FieldSelector{{"field1"}},
			expectedResponse: `{"field1": "plainText"}`,
		},
		"missing fields can be skipped": {
			mutator: stubMutator{
				mutateResponse: `"encryptedText"`,
			},
			responseBody:     `{"field1": "plainText"}`,
			skipFields:       FieldSelector{{"field2"}},
			expectedResponse: `{"field1": "encryptedText"}`,
		},
		"selected fields are skipped": {
			mutator: stubMutator{
				mutateResponse: `"encryptedText"`,
			},
			responseBody:     `{"field1": "plainText", "field2": "plainText2"}`,
			skipFields:       FieldSelector{{"field1"}},
			expectedResponse: `{"field1": "plainText", "field2": "encryptedText"}`,
		},
		"mutate from nested JSON": {
			mutator: stubMutator{
				mutateResponse: `"encryptedText"`,
			},
			responseBody:     `{"field1": {"nestedField": "plainText"}}`,
			expectedResponse: `{"field1": "encryptedText"}`,
		},
		"mutate to nested JSON": {
			mutator: stubMutator{
				mutateResponse: `{"nestedField": "plainText"}`,
			},
			responseBody:     `{"field1": "encryptedText"}`,
			expectedResponse: `{"field1": {"nestedField": "plainText"}}`,
		},
		"mutate error": {
			mutator: stubMutator{
				mutateErr: assert.AnError,
			},
			responseBody: `{"field1": "plainText"}`,
			wantErr:      true,
		},
		"mutate array fields": {
			mutator: stubMutator{
				mutateResponse: `"encryptedText"`,
			},
			responseBody:     `{"field1": [{"key": "plainText", "skip": "plainText"}, {"key": "plainText", "skip": "plainText"}]}`,
			skipFields:       FieldSelector{{"field1", "#", "skip"}},
			expectedResponse: `{"field1": [{"key": "encryptedText", "skip": "plainText"}, {"key": "encryptedText", "skip": "plainText"}]}`,
		},
		"skip deeply nested arrays": {
			mutator: stubMutator{
				mutateResponse: `"encryptedText"`,
			},
			responseBody:     `{"field1": [{"nested": [{"skip": "plainText", "mutate": "plainText1"}]}, {"nested": [{"skip": "plainText", "mutate": "plainText2"}]}]}`,
			skipFields:       FieldSelector{{"field1", "#", "nested", "#", "skip"}},
			expectedResponse: `{"field1": [{"nested": [{"skip": "plainText", "mutate": "encryptedText"}]}, {"nested": [{"skip": "plainText", "mutate": "encryptedText"}]}]}`,
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			mutator := WithFullJSONResponseMutation(tc.mutator.mutate, tc.skipFields, false)

			body, err := mutator.Mutate([]byte(tc.responseBody))
			if tc.wantErr {
				assert.Error(err)
				return
			}

			assert.NoError(err)
			assert.JSONEq(tc.expectedResponse, string(body))
		})
	}

	streamingCases := map[string]struct {
		mutator stubMutator
	}{
		"streaming mutation": {
			mutator: stubMutator{
				mutateResponse: `"encryptedText"`,
			},
		},
		"streaming large field mutation": {
			mutator: stubMutator{
				mutateResponse: fmt.Sprintf("\"%s\"", bytes.Repeat([]byte("encryptedText"), 10000)),
			},
		},
	}
	for name, tc := range streamingCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			mutator := WithFullJSONResponseMutation(tc.mutator.mutate, nil, false)

			msgChan := make(chan string)
			reader := &fakeReader{
				msgChan: msgChan,
			}

			messageParts := 10
			go func() {
				for i := range messageParts {
					msgChan <- fmt.Sprintf(`%d: {"field1": "plainText"}`, i) + "\n\n"
				}
				msgChan <- "data: [DONE]\n\n"
				close(msgChan)
			}()

			response := &bytes.Buffer{}
			_, err := io.Copy(response, mutator.Reader(reader))
			assert.NoError(err)
			responseParts := strings.Split(strings.TrimRight(response.String(), "\n"), "\n\n") // trim the last newline, so that we don't get an empty final part
			assert.Len(responseParts, messageParts+1)
			for i := range messageParts {
				assert.Equal(fmt.Sprintf(`%d: {"field1": %s}`, i, tc.mutator.mutateResponse), responseParts[i])
			}
			assert.Equal("data: [DONE]", responseParts[messageParts]) // assert the event stream message is not mutated
		})
	}
}

type fakeReader struct {
	msgChan chan string
}

func (f *fakeReader) Read(p []byte) (n int, err error) {
	msg, ok := <-f.msgChan
	if !ok {
		return 0, io.EOF
	}
	return copy(p, []byte(msg)), nil
}

func (f *fakeReader) Close() error {
	if _, ok := <-f.msgChan; ok {
		close(f.msgChan)
	}
	return nil
}

type stubMutator struct {
	mutateResponse string
	mutateErr      error
}

func (s stubMutator) mutate(_ string) (string, error) {
	return s.mutateResponse, s.mutateErr
}
