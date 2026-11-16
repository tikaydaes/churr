package api_test

import (
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

func TestLookupCodePoints(t *testing.T) {
	ts := testkit.NewServer(t)
	sam := ts.Member("sam")
	got := sam.Must(200, "GET", "/api/v1/unicode?code_points=1F600,U%2B10F2,a", nil)
	if got["version"] != "16.0.0" {
		t.Fatalf("version: %v", got)
	}
	list := arr(got, "code_points")
	want := [][3]string{{"U+1F600", "GRINNING FACE", "So"}, {"U+10F2", "GEORGIAN LETTER HIE", "Ll"}, {"U+000A", "LINE FEED (LF)", "Cc"}}
	for i, w := range want {
		c := list[i].(map[string]any)
		if c["code_point"] != w[0] || c["name"] != w[1] || c["category"] != w[2] {
			t.Errorf("%d: %v", i, c)
		}
	}
	if list[0].(map[string]any)["category_name"] != "Other Symbol" {
		t.Errorf("category name: %v", list[0])
	}
	sam.Refused(400, model.CodeInvalidValue, "GET", "/api/v1/unicode?code_points=110000", nil)
	sam.Refused(400, model.CodeInvalidValue, "GET", "/api/v1/unicode?code_points=", nil)
	sam.Refused(400, model.CodeInvalidValue, "GET", "/api/v1/unicode?code_points=1,2,3,4,5,6,7,8,9,A,B,C,D,E,F,10,11", nil)
}
