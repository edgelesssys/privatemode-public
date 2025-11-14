// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package forwarder

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func TestWithFormRequestMutation(t *testing.T) {
	testCases := map[string]struct {
		mutator      stubMutator
		request      func(*testing.T, *require.Assertions) *http.Request
		expectedForm map[string]string
		skipFields   FieldSelector
		wantErr      bool
	}{
		"all forms mutation": {
			mutator: stubMutator{
				mutateResponse: "mutated",
			},
			expectedForm: map[string]string{
				"field1": "mutated",
				"field2": "mutated",
			},
			request: func(t *testing.T, require *require.Assertions) *http.Request {
				body := &bytes.Buffer{}
				writer := multipart.NewWriter(body)
				require.NoError(writer.WriteField("field1", "plain text"))
				require.NoError(writer.WriteField("field2", "plain text"))
				require.NoError(writer.Close())

				req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://192.0.2.1", body)
				require.NoError(err)
				req.Header.Set("Content-Type", writer.FormDataContentType())
				return req
			},
		},
		"skip select fields": {
			mutator: stubMutator{
				mutateResponse: "mutated",
			},
			skipFields: FieldSelector{{"field1"}},
			expectedForm: map[string]string{
				"field1": "plain text",
				"field2": "mutated",
				"field3": "mutated",
			},
			request: func(t *testing.T, require *require.Assertions) *http.Request {
				body := &bytes.Buffer{}
				writer := multipart.NewWriter(body)
				require.NoError(writer.WriteField("field1", "plain text"))
				require.NoError(writer.WriteField("field2", "plain text"))
				require.NoError(writer.WriteField("field3", "plain text"))
				require.NoError(writer.Close())

				req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://192.0.2.1", body)
				require.NoError(err)
				req.Header.Set("Content-Type", writer.FormDataContentType())
				return req
			},
		},
		"form file mutation": {
			mutator: stubMutator{
				mutateResponse: "mutated",
			},
			expectedForm: map[string]string{
				"file1": "mutated",
				"file2": "plain text",
			},

			skipFields: FieldSelector{{"file2"}},
			request: func(t *testing.T, require *require.Assertions) *http.Request {
				body := &bytes.Buffer{}
				writer := multipart.NewWriter(body)

				fileWriter, err := writer.CreateFormFile("file1", "file.txt")
				require.NoError(err)
				_, err = fileWriter.Write([]byte("plain text"))
				require.NoError(err)

				fileWriter, err = writer.CreateFormFile("file2", "file.txt")
				require.NoError(err)
				_, err = fileWriter.Write([]byte("plain text"))
				require.NoError(err)

				require.NoError(writer.Close())

				req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://192.0.2.1", body)
				require.NoError(err)
				req.Header.Set("Content-Type", writer.FormDataContentType())
				return req
			},
		},
		"mutation error": {
			mutator: stubMutator{
				mutateErr: assert.AnError,
			},
			request: func(t *testing.T, require *require.Assertions) *http.Request {
				body := &bytes.Buffer{}
				writer := multipart.NewWriter(body)
				require.NoError(writer.WriteField("field1", "plain text"))
				require.NoError(writer.WriteField("field2", "plain text"))
				require.NoError(writer.Close())

				req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://192.0.2.1", body)
				require.NoError(err)
				req.Header.Set("Content-Type", writer.FormDataContentType())
				return req
			},
			wantErr: true,
		},
		"incorrect content type": {
			mutator: stubMutator{
				mutateResponse: "mutated",
			},
			request: func(t *testing.T, require *require.Assertions) *http.Request {
				body := &bytes.Buffer{}
				writer := multipart.NewWriter(body)
				require.NoError(writer.WriteField("field1", "plain text"))
				require.NoError(writer.WriteField("field2", "plain text"))
				require.NoError(writer.Close())

				req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://192.0.2.1", body)
				require.NoError(err)
				req.Header.Set("Content-Type", "garbage")
				return req
			},
			wantErr: true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			mutate := WithFormRequestMutation(tc.mutator.mutate, tc.skipFields, slog.Default())

			req := tc.request(t, require)
			err := mutate(req)
			if tc.wantErr {
				assert.Error(err)
				return
			}
			assert.NoError(err)

			handler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				err := r.ParseMultipartForm(64 * 1024 * 1024)
				assert.NoError(err)

				var formFields []string
				for key := range r.MultipartForm.Value {
					formFields = append(formFields, key)
					assert.Equal(tc.expectedForm[key], r.FormValue(key))
				}

				for key := range r.MultipartForm.File {
					formFields = append(formFields, key)
					file, _, err := r.FormFile(key)
					assert.NoError(err)
					defer file.Close()
					fileData, err := io.ReadAll(file)
					assert.NoError(err)
					assert.Equal(tc.expectedForm[key], string(fileData))
				}

				var expectedFields []string
				for key := range tc.expectedForm {
					expectedFields = append(expectedFields, key)
				}
				assert.ElementsMatch(expectedFields, formFields)
			})

			client := &http.Client{}
			server := httptest.NewServer(handler)
			defer server.Close()
			req.URL, err = url.Parse(server.URL)
			require.NoError(err)

			res, err := client.Do(req)
			require.NoError(err)
			defer res.Body.Close()
			assert.Equal(http.StatusOK, res.StatusCode)
		})
	}
}

func TestWithFullResponseMutation(t *testing.T) {
	testCases := map[string]struct {
		mutator          stubMutator
		responseBody     string
		expectedResponse string
		wantErr          bool
	}{
		"plain text mutation": {
			mutator: stubMutator{
				mutateResponse: "encryptedText",
			},
			responseBody:     "original text",
			expectedResponse: "encryptedText",
		},
		"JSON body mutation": {
			mutator: stubMutator{
				mutateResponse: "encryptedText",
			},
			responseBody:     `{"field": "original"}`,
			expectedResponse: "encryptedText",
		},
		"empty body": {
			mutator: stubMutator{
				mutateResponse: "",
			},
			responseBody:     "",
			expectedResponse: "",
		},
		"mutation error": {
			mutator: stubMutator{
				mutateErr: assert.AnError,
			},
			responseBody: "original text",
			wantErr:      true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			mutator := WithFullResponseMutation(tc.mutator.mutate)

			body, err := mutator.Mutate([]byte(tc.responseBody))
			if tc.wantErr {
				assert.Error(err)
				return
			}

			assert.NoError(err)
			assert.Equal(tc.expectedResponse, string(body))
		})
	}

	streamingCases := map[string]struct {
		mutator      stubMutator
		responseBody string
	}{
		"streaming JSON mutation": {
			mutator: stubMutator{
				mutateResponse: "encryptedText",
			},
			responseBody: `{"field1": "plainText"}`,
		},
		"streaming JSON large field mutation": {
			mutator: stubMutator{
				mutateResponse: string(bytes.Repeat([]byte("encryptedText"), 10000)),
			},
			responseBody: `{"field1": "plainText"}`,
		},
		"streaming text data mutation": {
			mutator: stubMutator{
				mutateResponse: "encryptedText",
			},
			responseBody: "plainText",
		},
		"streaming text data large field mutation": {
			mutator: stubMutator{
				mutateResponse: string(bytes.Repeat([]byte("encryptedText"), 10000)),
			},
			responseBody: "plainText",
		},
	}
	for name, tc := range streamingCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			mutator := WithFullResponseMutation(tc.mutator.mutate)

			msgChan := make(chan string)
			reader := &fakeReader{
				msgChan: msgChan,
			}

			messageParts := 10
			go func() {
				for i := range messageParts {
					msgChan <- fmt.Sprintf("%d: %s", i, tc.responseBody) + "\n\n"
				}
				close(msgChan)
			}()

			response := &bytes.Buffer{}
			_, err := io.Copy(response, mutator.Reader(reader))
			assert.NoError(err)
			responseParts := strings.Split(strings.TrimRight(response.String(), "\n"), "\n\n") // trim the last newline, so that we don't get an empty final part
			assert.Len(responseParts, messageParts)
			for i, part := range responseParts {
				assert.Equal(fmt.Sprintf("%d: %s", i, tc.mutator.mutateResponse), part)
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
