package runtime

import (
	"testing"

	"github.com/hyperxlab/tales/internal/model"
)

func TestFilterScenarios(t *testing.T) {
	t.Parallel()

	scenarios := []*model.Scenario{
		{Name: "alpha", Tags: []string{"smoke", "fast"}},
		{Name: "beta", Tags: []string{"smoke", "slow"}},
		{Name: "gamma", Tags: []string{"auth"}},
		{Name: "delta"}, // no tags
	}

	cases := []struct {
		name     string
		tags     []string
		scenario string
		want     []string
	}{
		{
			name: "no filter returns all scenarios",
			want: []string{"alpha", "beta", "gamma", "delta"},
		},
		{
			name: "single tag matches scenarios carrying it",
			tags: []string{"smoke"},
			want: []string{"alpha", "beta"},
		},
		{
			name: "multiple tags union (OR semantics)",
			tags: []string{"smoke", "auth"},
			want: []string{"alpha", "beta", "gamma"},
		},
		{
			name: "unknown tag matches nothing",
			tags: []string{"missing"},
			want: []string{},
		},
		{
			name:     "scenario name selects exactly one regardless of tags",
			scenario: "delta",
			want:     []string{"delta"},
		},
		{
			name:     "scenario name combined with tag must satisfy both",
			scenario: "alpha",
			tags:     []string{"smoke"},
			want:     []string{"alpha"},
		},
		{
			name:     "scenario name combined with non-matching tag drops it",
			scenario: "alpha",
			tags:     []string{"auth"},
			want:     []string{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := filterScenarios(scenarios, tc.tags, tc.scenario)

			names := make([]string, 0, len(got))
			for _, s := range got {
				names = append(names, s.Name)
			}

			if len(names) != len(tc.want) {
				t.Fatalf("got %d scenarios (%v), want %d (%v)", len(names), names, len(tc.want), tc.want)
			}

			for i, want := range tc.want {
				if names[i] != want {
					t.Fatalf("scenario[%d] = %q, want %q (full=%v)", i, names[i], want, names)
				}
			}
		})
	}
}
