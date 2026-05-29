package config

import "testing"

func TestNormalizeConfigLockedAssignsUniqueIDs(t *testing.T) {
	previous := Config
	defer func() { Config = previous }()

	Config = AppConfig{
		ZSpace: []ZSpaceConfig{{}, {ID: "dup"}},
		UGreen: []UGreenConfig{{ID: "dup"}},
		FnOs:   []FnOsConfig{{}},
	}

	normalizeConfigLocked()

	ids := map[string]struct{}{}
	for _, item := range Config.ZSpace {
		if item.ID == "" {
			t.Fatalf("expected zspace id to be populated")
		}
		if _, ok := ids[item.ID]; ok {
			t.Fatalf("duplicate id generated: %s", item.ID)
		}
		ids[item.ID] = struct{}{}
	}
	for _, item := range Config.UGreen {
		if item.ID == "" {
			t.Fatalf("expected ugreen id to be populated")
		}
		if _, ok := ids[item.ID]; ok {
			t.Fatalf("duplicate id generated: %s", item.ID)
		}
		ids[item.ID] = struct{}{}
	}
	for _, item := range Config.FnOs {
		if item.ID == "" {
			t.Fatalf("expected fnos id to be populated")
		}
		if _, ok := ids[item.ID]; ok {
			t.Fatalf("duplicate id generated: %s", item.ID)
		}
		ids[item.ID] = struct{}{}
	}
}

func TestNormalizeConfigLockedDefaultsIntervals(t *testing.T) {
	previous := Config
	defer func() { Config = previous }()

	Config = AppConfig{}

	normalizeConfigLocked()

	if Config.IntervalMinutes != DefaultIntervalMinutes {
		t.Fatalf("expected notification interval default %v, got %v", DefaultIntervalMinutes, Config.IntervalMinutes)
	}
	if Config.SystemStatusIntervalMinutes != DefaultSystemStatusIntervalMinutes {
		t.Fatalf("expected system status interval default %v, got %v", DefaultSystemStatusIntervalMinutes, Config.SystemStatusIntervalMinutes)
	}
}

func TestMergeWithExistingSensitiveFieldsMatchesByID(t *testing.T) {
	existing := AppConfig{
		ZSpace: []ZSpaceConfig{
			{ID: "zs-1", Cookie: "cookie-1"},
			{ID: "zs-2", Cookie: "cookie-2"},
		},
		UGreen: []UGreenConfig{
			{ID: "ug-1", Password: "pass-1"},
		},
		FnOs: []FnOsConfig{
			{ID: "fn-1", Password: "pass-1", Cookie: "cookie-1"},
		},
	}

	incoming := AppConfig{
		ZSpace: []ZSpaceConfig{
			{ID: "zs-2"},
			{ID: "zs-1", Cookie: "new-cookie"},
		},
		UGreen: []UGreenConfig{
			{ID: "ug-1"},
		},
		FnOs: []FnOsConfig{
			{ID: "fn-1"},
		},
	}

	merged := MergeWithExistingSensitiveFields(existing, incoming)

	if got := merged.ZSpace[0].Cookie; got != "cookie-2" {
		t.Fatalf("expected zspace cookie to be preserved by id, got %q", got)
	}
	if got := merged.ZSpace[1].Cookie; got != "new-cookie" {
		t.Fatalf("expected explicit zspace cookie to win, got %q", got)
	}
	if got := merged.UGreen[0].Password; got != "pass-1" {
		t.Fatalf("expected ugreen password to be preserved by id, got %q", got)
	}
	if got := merged.FnOs[0].Password; got != "pass-1" {
		t.Fatalf("expected fnos password to be preserved by id, got %q", got)
	}
	if got := merged.FnOs[0].Cookie; got != "cookie-1" {
		t.Fatalf("expected fnos cookie to be preserved by id, got %q", got)
	}
}

func TestSanitizedConfigForWebKeepsIDs(t *testing.T) {
	previous := Config
	defer func() { Config = previous }()

	Config = AppConfig{
		ZSpace: []ZSpaceConfig{{ID: "zs-1", Cookie: "cookie-1"}},
		UGreen: []UGreenConfig{{ID: "ug-1", Password: "pass-1"}},
		FnOs:   []FnOsConfig{{ID: "fn-1", Password: "pass-1", Cookie: "cookie-1"}},
	}

	sanitized := SanitizedConfigForWeb()

	if got := sanitized.ZSpace[0].ID; got != "zs-1" {
		t.Fatalf("expected zspace id to remain visible, got %q", got)
	}
	if got := sanitized.ZSpace[0].Cookie; got != "" {
		t.Fatalf("expected zspace cookie to be cleared, got %q", got)
	}
	if got := sanitized.UGreen[0].ID; got != "ug-1" {
		t.Fatalf("expected ugreen id to remain visible, got %q", got)
	}
	if got := sanitized.UGreen[0].Password; got != "" {
		t.Fatalf("expected ugreen password to be cleared, got %q", got)
	}
	if got := sanitized.FnOs[0].ID; got != "fn-1" {
		t.Fatalf("expected fnos id to remain visible, got %q", got)
	}
	if got := sanitized.FnOs[0].Password; got != "" {
		t.Fatalf("expected fnos password to be cleared, got %q", got)
	}
	if got := sanitized.FnOs[0].Cookie; got != "" {
		t.Fatalf("expected fnos cookie to be cleared, got %q", got)
	}
}
