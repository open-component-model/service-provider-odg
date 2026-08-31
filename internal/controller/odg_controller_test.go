package controller

import (
	"bytes"
	"encoding/json"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

func jsonOf(t *testing.T, v any) *apiextensionsv1.JSON {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("jsonOf: %v", err)
	}
	return &apiextensionsv1.JSON{Raw: raw}
}

func unmarshal(t *testing.T, j *apiextensionsv1.JSON) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(j.Raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return m
}

func TestMergeHelmValues(t *testing.T) {
	tests := []struct {
		name    string
		base    *apiextensionsv1.JSON
		overlay *apiextensionsv1.JSON
		want    map[string]any
	}{
		{
			name:    "nil overlay returns base",
			base:    jsonOf(t, map[string]any{"a": 1}),
			overlay: nil,
			want:    map[string]any{"a": float64(1)},
		},
		{
			name:    "nil base returns overlay",
			base:    nil,
			overlay: jsonOf(t, map[string]any{"b": 2}),
			want:    map[string]any{"b": float64(2)},
		},
		{
			name:    "both nil returns nil",
			base:    nil,
			overlay: nil,
			want:    nil,
		},
		{
			name:    "overlay adds new keys",
			base:    jsonOf(t, map[string]any{"a": 1}),
			overlay: jsonOf(t, map[string]any{"b": 2}),
			want:    map[string]any{"a": float64(1), "b": float64(2)},
		},
		{
			name:    "overlay scalar overwrites base",
			base:    jsonOf(t, map[string]any{"a": 1}),
			overlay: jsonOf(t, map[string]any{"a": 99}),
			want:    map[string]any{"a": float64(99)},
		},
		{
			name:    "nested maps are merged recursively",
			base:    jsonOf(t, map[string]any{"x": map[string]any{"a": 1, "b": 2}}),
			overlay: jsonOf(t, map[string]any{"x": map[string]any{"b": 99, "c": 3}}),
			want:    map[string]any{"x": map[string]any{"a": float64(1), "b": float64(99), "c": float64(3)}},
		},
		{
			name:    "secret keys win over configmap keys",
			base:    jsonOf(t, map[string]any{"secrets": map[string]any{"db": map[string]any{"password": "old"}}}),
			overlay: jsonOf(t, map[string]any{"secrets": map[string]any{"db": map[string]any{"password": "new"}}}),
			want:    map[string]any{"secrets": map[string]any{"db": map[string]any{"password": "new"}}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := mergeHelmValues(tc.base, tc.overlay)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.want == nil {
				if got != nil {
					t.Errorf("want nil, got %s", got.Raw)
				}
				return
			}
			gotMap := unmarshal(t, got)
			wantRaw, _ := json.Marshal(tc.want)
			gotRaw, _ := json.Marshal(gotMap)
			if !bytes.Equal(wantRaw, gotRaw) {
				t.Errorf("want %s, got %s", wantRaw, gotRaw)
			}
		})
	}
}

func TestDataToJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		wantNil bool
		wantKey string
		wantVal any
	}{
		{
			name:    "empty input returns nil",
			input:   nil,
			wantNil: true,
		},
		{
			name:    "valid JSON passes through",
			input:   []byte(`{"foo":"bar"}`),
			wantKey: "foo",
			wantVal: "bar",
		},
		{
			name:    "valid YAML is converted",
			input:   []byte("foo: bar\n"),
			wantKey: "foo",
			wantVal: "bar",
		},
		{
			name:    "nested YAML is converted",
			input:   []byte("extensions_cfg:\n  key: val\n"),
			wantKey: "extensions_cfg",
			wantVal: map[string]any{"key": "val"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := dataToJSON(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantNil {
				if got != nil {
					t.Errorf("want nil, got %s", got.Raw)
				}
				return
			}
			m := unmarshal(t, got)
			val, ok := m[tc.wantKey]
			if !ok {
				t.Errorf("key %q missing from result %s", tc.wantKey, got.Raw)
				return
			}
			wantRaw, _ := json.Marshal(tc.wantVal)
			gotRaw, _ := json.Marshal(val)
			if !bytes.Equal(wantRaw, gotRaw) {
				t.Errorf("key %q: want %s, got %s", tc.wantKey, wantRaw, gotRaw)
			}
		})
	}
}
