package api_test

import (
	"encoding/json"
	"testing"

	"github.com/ko5tas/t212/internal/api"
)

func TestReturnInfo_JSON(t *testing.T) {
	ri := api.ReturnInfo{
		TotalBought:    100.00,
		TotalSold:      35.00,
		TotalDividends: 7.30,
		Return:         42.30,
		ReturnPct:      42.30,
	}
	b, err := json.Marshal(ri)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got api.ReturnInfo
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Return != 42.30 {
		t.Errorf("Return: got %v, want 42.30", got.Return)
	}
}
