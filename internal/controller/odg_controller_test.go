package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	apiv1alpha1 "github.com/open-component-model/service-provider-odg/api/v1alpha1"
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

func TestIsExtensionEnabled(t *testing.T) {
	tests := []struct {
		name   string
		extCfg map[string]any
		key    string
		want   bool
	}{
		{
			name:   "nil extCfg returns false",
			extCfg: nil,
			key:    "blackduck",
			want:   false,
		},
		{
			name:   "absent key returns false",
			extCfg: map[string]any{"other": map[string]any{"enabled": true}},
			key:    "blackduck",
			want:   false,
		},
		{
			name:   "entry with enabled=true returns true",
			extCfg: map[string]any{"blackduck": map[string]any{"enabled": true}},
			key:    "blackduck",
			want:   true,
		},
		{
			name:   "entry with enabled=false returns false",
			extCfg: map[string]any{"blackduck": map[string]any{"enabled": false}},
			key:    "blackduck",
			want:   false,
		},
		{
			name:   "entry without enabled field returns true",
			extCfg: map[string]any{"blackduck": map[string]any{"some_config": "value"}},
			key:    "blackduck",
			want:   true,
		},
		{
			name:   "entry with empty map returns true",
			extCfg: map[string]any{"blackduck": map[string]any{}},
			key:    "blackduck",
			want:   true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isExtensionEnabled(tc.extCfg, tc.key); got != tc.want {
				t.Errorf("isExtensionEnabled(%v, %q) = %v, want %v", tc.extCfg, tc.key, got, tc.want)
			}
		})
	}
}

func TestSyncExtensionsFromBootstrapping(t *testing.T) {
	r := &ODGReconciler{} // no cluster clients needed: ODG has no ConfigurationRef/SecretsRef
	svcobj := &apiv1alpha1.ODG{}

	chartURL := "oci://example.com/chart"
	makeChart := func(name string, values any) apiv1alpha1.ODGChart {
		ch := apiv1alpha1.ODGChart{ChartName: name, ChartURL: &chartURL}
		if values != nil {
			ch.HelmValues = jsonOf(t, values)
		}
		return ch
	}

	tests := []struct {
		name           string
		charts         []apiv1alpha1.ODGChart
		wantCharts     []string       // chart names expected to survive filtering
		wantExtensions map[string]any // expected top-level keys in extensions helmValues
	}{
		{
			name: "gated charts absent from extCfg are removed",
			charts: []apiv1alpha1.ODGChart{
				makeChart("bootstrapping", map[string]any{
					"extensions_cfg": map[string]any{},
				}),
				makeChart("blackduck", nil),
				makeChart("sla-report", nil),
				makeChart("findings-report", nil),
				makeChart("extensions", nil),
			},
			wantCharts:     []string{"bootstrapping", "extensions"},
			wantExtensions: nil,
		},
		{
			name: "gated charts with enabled=true are kept",
			charts: []apiv1alpha1.ODGChart{
				makeChart("bootstrapping", map[string]any{
					"extensions_cfg": map[string]any{
						"blackduck":              map[string]any{"enabled": true},
						"sla_violation_profiler": map[string]any{"enabled": true},
						"findings_report":        map[string]any{"enabled": true},
					},
				}),
				makeChart("blackduck", nil),
				makeChart("sla-report", nil),
				makeChart("findings-report", nil),
				makeChart("extensions", nil),
			},
			wantCharts: []string{"bootstrapping", "blackduck", "sla-report", "findings-report", "extensions"},
			wantExtensions: map[string]any{
				"blackduck":              map[string]any{"enabled": true},
				"sla-violation-profiler": map[string]any{"enabled": true},
				"findings-report":        map[string]any{"enabled": true},
			},
		},
		{
			name: "gated chart with enabled=false is removed",
			charts: []apiv1alpha1.ODGChart{
				makeChart("bootstrapping", map[string]any{
					"extensions_cfg": map[string]any{
						"blackduck": map[string]any{"enabled": false},
					},
				}),
				makeChart("blackduck", nil),
				makeChart("extensions", nil),
			},
			wantCharts:     []string{"bootstrapping", "extensions"},
			wantExtensions: map[string]any{"blackduck": map[string]any{"enabled": false}},
		},
		{
			name: "entry without enabled field keeps chart and sets enabled=true in extensions",
			charts: []apiv1alpha1.ODGChart{
				makeChart("bootstrapping", map[string]any{
					"extensions_cfg": map[string]any{
						"blackduck": map[string]any{"some_cfg": "val"},
					},
				}),
				makeChart("blackduck", nil),
				makeChart("extensions", nil),
			},
			wantCharts:     []string{"bootstrapping", "blackduck", "extensions"},
			wantExtensions: map[string]any{"blackduck": map[string]any{"enabled": true}},
		},
		{
			name: "extensions overlay merges on top of existing extensions values",
			charts: []apiv1alpha1.ODGChart{
				makeChart("bootstrapping", map[string]any{
					"extensions_cfg": map[string]any{
						"cache_manager": map[string]any{"enabled": true},
					},
				}),
				makeChart("extensions", map[string]any{
					"global":        map[string]any{"image": map[string]any{"tag": "1.0"}},
					"cache-manager": map[string]any{"enabled": false},
				}),
			},
			wantCharts: []string{"bootstrapping", "extensions"},
			wantExtensions: map[string]any{
				"global":        map[string]any{"image": map[string]any{"tag": "1.0"}},
				"cache-manager": map[string]any{"enabled": true},
			},
		},
		{
			name: "no bootstrapping chart is a no-op",
			charts: []apiv1alpha1.ODGChart{
				makeChart("extensions", nil),
				makeChart("blackduck", nil),
			},
			wantCharts:     []string{"extensions", "blackduck"},
			wantExtensions: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pc := &apiv1alpha1.ProviderConfig{}
			pc.Spec.Charts = make([]apiv1alpha1.ODGChart, len(tc.charts))
			copy(pc.Spec.Charts, tc.charts)

			if err := r.syncExtensionsFromBootstrapping(context.Background(), svcobj, pc); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			gotNames := make([]string, len(pc.Spec.Charts))
			for i, ch := range pc.Spec.Charts {
				gotNames[i] = ch.ChartName
			}
			wantSet := make(map[string]bool, len(tc.wantCharts))
			for _, n := range tc.wantCharts {
				wantSet[n] = true
			}
			if len(gotNames) != len(tc.wantCharts) {
				t.Errorf("charts after sync: got %v, want %v", gotNames, tc.wantCharts)
			} else {
				for _, n := range gotNames {
					if !wantSet[n] {
						t.Errorf("unexpected chart %q in result %v", n, gotNames)
					}
				}
			}

			// Check extensions values.
			var extValues *apiextensionsv1.JSON
			for _, ch := range pc.Spec.Charts {
				if ch.ChartName == "extensions" {
					extValues = ch.HelmValues
					break
				}
			}
			if tc.wantExtensions == nil {
				if extValues != nil {
					gotMap := unmarshal(t, extValues)
					// allow non-nil only if all keys match expected (empty map case)
					if len(gotMap) != 0 {
						t.Errorf("expected no extensions values, got %s", extValues.Raw)
					}
				}
				return
			}
			if extValues == nil {
				t.Fatalf("expected extensions values, got nil")
			}
			gotMap := unmarshal(t, extValues)
			wantRaw, _ := json.Marshal(tc.wantExtensions)
			// Check each expected key individually for clearer errors.
			for k, wantVal := range tc.wantExtensions {
				gotVal, ok := gotMap[k]
				if !ok {
					t.Errorf("extensions values missing key %q; got %s", k, extValues.Raw)
					continue
				}
				a, _ := json.Marshal(wantVal)
				b, _ := json.Marshal(gotVal)
				if !bytes.Equal(a, b) {
					t.Errorf("extensions[%q]: want %s, got %s", k, a, b)
				}
			}
			_ = wantRaw
		})
	}
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
