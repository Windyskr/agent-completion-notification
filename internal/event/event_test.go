package event

import "testing"

func TestProject(t *testing.T) {
	cases := map[string]string{
		"/Users/me/Codespace/acn": "acn",
		"/work/proj/":             "proj",
		"":                        "",
	}
	for cwd, want := range cases {
		if got := (Event{Cwd: cwd}).Project(); got != want {
			t.Errorf("Project(%q) = %q, 期望 %q", cwd, got, want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	cases := map[int64]string{
		0:         "",
		-1:        "",
		5_000:     "5s",
		65_000:    "1m5s",
		3_600_000: "1h0m",
		7_320_000: "2h2m",
	}
	for ms, want := range cases {
		if got := FormatDuration(ms); got != want {
			t.Errorf("FormatDuration(%d) = %q, 期望 %q", ms, got, want)
		}
	}
}
