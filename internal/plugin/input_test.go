package plugin

import "testing"

func TestParseQueryInput(t *testing.T) {
	tests := []struct {
		name    string
		command string
		args    []string
		want    queryInput
		wantErr bool
	}{
		{name: "city grade brand", command: "油价", args: []string{"深圳", "95", "中石化"}, want: queryInput{Area: "深圳", Grade: "95", BrandID: "sinopec", BrandName: "中国石化"}},
		{name: "district and diesel", command: "油价", args: []string{"深圳", "南山", "0号柴油"}, want: queryInput{Area: "深圳南山", Grade: "0"}},
		{name: "station default area", command: "油站", args: []string{"中石油"}, want: queryInput{Area: "北京", BrandID: "petrochina", BrandName: "中国石油", StationsOnly: true}},
		{name: "conflicting grades", command: "油价", args: []string{"北京", "92", "95"}, wantErr: true},
		{name: "conflicting brands", command: "油价", args: []string{"北京", "中石油", "中石化"}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseQueryInput(test.command, test.args, "北京")
			if test.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseQueryInput: %v", err)
			}
			if got != test.want {
				t.Fatalf("input = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestNormalizeRegionName(t *testing.T) {
	tests := map[string]string{
		"广东省":     "广东",
		"广西壮族自治区": "广西",
		"北京市":     "北京",
		"内蒙古自治区":  "内蒙古",
	}
	for input, want := range tests {
		if got := normalizeRegionName(input); got != want {
			t.Fatalf("normalizeRegionName(%q) = %q, want %q", input, got, want)
		}
	}
}
