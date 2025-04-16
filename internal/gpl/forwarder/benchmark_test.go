package forwarder

import (
	"bytes"
	"testing"

	"github.com/edgelesssys/continuum/internal/gpl/crypto"
	"github.com/stretchr/testify/require"
)

var benchmarkJSONData = []byte(`{
	"test": "test",
	"field1":{
		"field1.2": "someValue",
		"field2": "field2Value",
		"intValue": 3,
		"doubleNested": {
			"nestedField": "nestedValue",
			"nestedField2": "nestedValue2"
		}
	},
	"arrayData": [
		{
			"arrayField1": "field1Value"
		},
		{
			"arrayField2": "field2Value"
		}
	],
	"plainStruct": {
		"field1": "field1Value",
		"field2": "field2Value"
	},
	"field2": "field2Value",
	"plainField": "skip me",
	"plainInt": 3,
	"intValue": 3
}`)

func BenchmarkAllJSONFieldMutation(b *testing.B) {
	require := require.New(b)

	selector := FieldSelector{
		{"field1", "field2"},
		{"field1", "intValue"},
		{"field1", "doubleNested", "nestedField"},
		{"plainField"},
		{"plainInt"},
		{"plainStruct"},
	}

	for b.Loop() {
		key := bytes.Repeat([]byte{byte(b.N % 0xFF)}, 16)
		rc, err := crypto.NewRequestCipher(key, "testing")
		require.NoError(err)
		_, err = mutateAllJSONFields(benchmarkJSONData, rc.Encrypt, selector)
		require.NoError(err)
	}
}

func BenchmarkJSONFieldMutation(b *testing.B) {
	require := require.New(b)

	selector := FieldSelector{
		{"test"},
		{"field1", "field1\\.2"},
		{"field1", "intValue"},
		{"field1", "doubleNested", "nestedField2"},
		{"arrayData"},
		{"field2"},
		{"intValue"},
	}

	for b.Loop() {
		key := bytes.Repeat([]byte{byte(b.N % 0xFF)}, 16)
		rc, err := crypto.NewRequestCipher(key, "testing")
		require.NoError(err)
		_, err = mutateSelectJSONFields(benchmarkJSONData, rc.Encrypt, selector)
		require.NoError(err)
	}
}
