package config

import (
	"encoding/json"
	"testing"
)

func TestWeaponReleaseItemOrderAndDefaults(t *testing.T) {
	var games MiniGames
	data := []byte(`{"WeaponRelease":{"Active":true,"Items":{"567":{"Name":"GALIL","Duration":30},"566":{"Name":"DAGGER","Count":2}},"Characters":["A","~"]}}`)
	if err := json.Unmarshal(data, &games); err != nil {
		t.Fatal(err)
	}
	release := games.WeaponRelease
	if !release.Active || len(release.Items) != 2 || len(release.Characters) != 2 {
		t.Fatalf("weapon release %+v", release)
	}
	if release.Items[0].ID != 567 || release.Items[0].Name != "GALIL" || release.Items[0].Count != 1 || release.Items[0].Duration != 30 {
		t.Fatalf("first item %+v", release.Items[0])
	}
	if release.Items[1].ID != 566 || release.Items[1].Count != 2 {
		t.Fatalf("second item %+v", release.Items[1])
	}
}
