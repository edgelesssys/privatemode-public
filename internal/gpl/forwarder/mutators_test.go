// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: GPL-3.0-only

package forwarder

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWithJSONRequestMutation(t *testing.T) {
	testCases := map[string]struct {
		mutator          stubMutator
		fields           FieldSelector
		requestBody      string
		expectedResponse string
		wantErr          bool
	}{
		"single field simple string": {
			mutator: stubMutator{
				mutateResponse: "encryptedText",
			},
			requestBody:      `{"field1": "plainText"}`,
			fields:           FieldSelector{"field1": SimpleValue},
			expectedResponse: `{"field1": "encryptedText"}`,
		},
		"single field nested JSON": {
			mutator: stubMutator{
				mutateResponse: `{"nestedField": "plainText"}`,
			},
			requestBody:      `{"field1": "encryptedText"}`,
			fields:           FieldSelector{"field1": NestedValue},
			expectedResponse: `{"field1": {"nestedField": "plainText"}}`,
		},
		"multiple fields": {
			mutator: stubMutator{
				mutateResponse: "plainText",
			},
			requestBody:      `{"field1": "encryptedText1", "field2": "encryptedText2"}`,
			fields:           FieldSelector{"field1": SimpleValue, "field2": SimpleValue},
			expectedResponse: `{"field1": "plainText", "field2": "plainText"}`,
		},
		"mutate error": {
			mutator: stubMutator{
				mutateErr: assert.AnError,
			},
			requestBody: `{"field1": "encryptedText"}`,
			fields:      FieldSelector{"field1": SimpleValue},
			wantErr:     true,
		},
		"missing fields are skipped": {
			mutator: stubMutator{
				mutateResponse: "plainText",
			},
			requestBody:      `{"field1": "encryptedText1"}`,
			fields:           FieldSelector{"field1": SimpleValue, "field2": SimpleValue},
			expectedResponse: `{"field1": "plainText"}`,
		},
		"not selected fields are skipped": {
			mutator: stubMutator{
				mutateResponse: "plainText",
			},
			requestBody:      `{"field1": "encryptedText1", "field2": "encryptedText2"}`,
			fields:           FieldSelector{"field2": SimpleValue},
			expectedResponse: `{"field1": "encryptedText1", "field2": "plainText"}`,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			mutate := WithJSONRequestMutation(tc.mutator.mutate, tc.fields, slog.Default())
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

	responseMutator := WithJSONResponseMutation(
		stubMutator{
			mutateResponse: "encryptedText",
		}.mutate,
		FieldSelector{"field1": SimpleValue},
	)
	sut := responseMutator(readCloser)

	res := &bytes.Buffer{}
	_, err := io.Copy(res, sut)
	assert.NoError(err)
	expectedResponse := fmt.Sprintf(dataFmt, "encryptedText")
	assert.Equal(expectedResponse, res.String())
}

func TestWithResponseMutation(t *testing.T) {
	testCases := map[string]struct {
		mutator          stubMutator
		responseBody     string
		fields           FieldSelector
		expectedResponse string
		wantErr          bool
	}{
		"single field mutation": {
			mutator: stubMutator{
				mutateResponse: "encryptedText",
			},
			responseBody:     `{"field1": "plainText"}`,
			fields:           FieldSelector{"field1": SimpleValue},
			expectedResponse: `{"field1": "encryptedText"}`,
		},
		"multiple fields": {
			mutator: stubMutator{
				mutateResponse: "encryptedText",
			},
			responseBody:     `{"field1": "plainText1", "field2": "plainText2"}`,
			fields:           FieldSelector{"field1": SimpleValue, "field2": SimpleValue},
			expectedResponse: `{"field1": "encryptedText", "field2": "encryptedText"}`,
		},
		"missing fields are skipped": {
			mutator: stubMutator{
				mutateResponse: "encryptedText",
			},
			responseBody:     `{"field1": "plainText"}`,
			fields:           FieldSelector{"field2": SimpleValue},
			expectedResponse: `{"field1": "plainText"}`,
		},
		"not selected fields are skipped": {
			mutator: stubMutator{
				mutateResponse: "encryptedText",
			},
			responseBody:     `{"field1": "plainText", "field2": "plainText2"}`,
			fields:           FieldSelector{"field2": SimpleValue},
			expectedResponse: `{"field1": "plainText", "field2": "encryptedText"}`,
		},
		"mutate from nested JSON": {
			mutator: stubMutator{
				mutateResponse: "encryptedText",
			},
			responseBody:     `{"field1": {"nestedField": "plainText"}}`,
			fields:           FieldSelector{"field1": SimpleValue},
			expectedResponse: `{"field1": "encryptedText"}`,
		},
		"mutate to nested JSON": {
			mutator: stubMutator{
				mutateResponse: `{"nestedField": "plainText"}`,
			},
			responseBody:     `{"field1": "encryptedText"}`,
			fields:           FieldSelector{"field1": NestedValue},
			expectedResponse: `{"field1": {"nestedField": "plainText"}}`,
		},
		"mutate error": {
			mutator: stubMutator{
				mutateErr: assert.AnError,
			},
			responseBody: `{"field1": "plainText"}`,
			fields:       FieldSelector{"field1": SimpleValue},
			wantErr:      true,
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			mutator := WithJSONResponseMutation(tc.mutator.mutate, tc.fields)
			response := io.NopCloser(bytes.NewBufferString(tc.responseBody))

			body, err := io.ReadAll(mutator(response))
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
				mutateResponse: "encryptedText",
			},
		},
		"streaming large field mutation": {
			mutator: stubMutator{
				mutateResponse: string(bytes.Repeat([]byte("encryptedText"), 10000)),
			},
		},
	}
	for name, tc := range streamingCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			mutator := WithJSONResponseMutation(tc.mutator.mutate, FieldSelector{"field1": SimpleValue})

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
			_, err := io.Copy(response, mutator(reader))
			assert.NoError(err)
			responseParts := strings.Split(strings.TrimRight(response.String(), "\n"), "\n\n") // trim the last newline, so that we don't get an empty final part
			assert.Len(responseParts, messageParts)
			for i, part := range responseParts {
				assert.Equal(fmt.Sprintf(`%d: {"field1": "%s"}`, i, tc.mutator.mutateResponse), part)
			}
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
