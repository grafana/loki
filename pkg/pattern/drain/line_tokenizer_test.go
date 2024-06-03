package drain

import (
	"reflect"
	"testing"
)

func TestSplittingTokenizer_Tokenize(t *testing.T) {
	tokenizer := splittingTokenizer{}

	tests := []struct {
		name string
		line string
		want []string
	}{
		{
			name: "Test with equals sign",
			line: "key1=value1 key2=value2",
			want: []string{"key1=", "value1", "key2=", "value2"},
		},
		{
			name: "Test with colon",
			line: "key1:value1 key2:value2",
			want: []string{"key1:", "value1", "key2:", "value2"},
		},
		{
			name: "Test with mixed delimiters, more = than :",
			line: "key1=value1 key2:value2 key3=value3",
			want: []string{"key1=", "value1", "key2:value2", "key3=", "value3"},
		},
		{
			name: "Test with mixed delimiters, more : than =",
			line: "key1:value1 key2:value2 key3=value3",
			want: []string{"key1:", "value1", "key2:", "value2", "key3=value3"},
		},
		{
			name: "Dense json",
			line: `{"key1":"value1","key2":"value2","key3":"value3"}`,
			want: []string{`{"key1":`, `"value1","key2":`, `"value2","key3":`, `"value3"}`},
		},
		{
			name: "json with spaces",
			line: `{"key1":"value1", "key2":"value2", "key3":"value3"}`,
			want: []string{`{"key1":`, `"value1",`, `"key2":`, `"value2",`, `"key3":`, `"value3"}`},
		},
		{
			name: "logfmt multiword values",
			line: `key1=value1 key2=value2 msg="this is a message"`,
			want: []string{"key1=", "value1", "key2=", "value2", "msg=", `"this`, "is", "a", `message"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tokenizer.Tokenize(tt.line); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("splittingTokenizer.Tokenize() = %v, want %v", got, tt.want)
			}
		})
	}
}
