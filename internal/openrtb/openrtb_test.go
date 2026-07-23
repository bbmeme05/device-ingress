package openrtb

import "testing"

func TestToDeviceRecordMapsOpenRTBAndroid(t *testing.T) {
	request := BidRequest{
		ID:       "request-1",
		ClientID: "2501",
		SupplyID: "98",
		App: App{
			Bundle:     "com.viber.voip",
			Categories: []string{"IAB19"},
			Publisher:  Publisher{ID: "98"},
		},
		Device: Device{
			OS:             "Android",
			IP:             "112.198.254.159",
			UA:             "Mozilla/5.0 (Linux; Android 12)",
			IFA:            "550e8400-e29b-41d4-a716-446655440000",
			Make:           "Huawei",
			Model:          "JLN-LX1",
			OSVersion:      "12",
			Carrier:        "Globe Telecom",
			ConnectionType: 1,
			Geo:            Geo{Country: "phl", City: "Quezon City", Region: "00"},
		},
		Imp:    []Imp{{ID: "1", TagID: "banner", Banner: Banner{Width: 300, Height: 250}}},
		Source: Source{TransactionID: "transaction-1"},
	}

	record, err := request.ToDeviceRecord()
	if err != nil {
		t.Fatalf("ToDeviceRecord() error = %v", err)
	}
	if record.OS != "android" || record.IFA != "550e8400-e29b-41d4-a716-446655440000" {
		t.Fatalf("unexpected device identity: %#v", record)
	}
	if record.AppID != "com.viber.voip" || record.AdxID != "2501" || record.MediaSource != "98" {
		t.Fatalf("unexpected app/source mapping: %#v", record)
	}
	if record.AdWidth != "300" || record.AdHeight != "250" || record.AdPlacement != "banner" {
		t.Fatalf("unexpected impression mapping: %#v", record)
	}
}

func TestToDeviceRecordRejectsAndroidWithoutIFA(t *testing.T) {
	request := BidRequest{
		App:    App{Bundle: "com.example.app"},
		Device: Device{OS: "android", IP: "203.0.113.10", UA: "Mozilla/5.0"},
		Imp:    []Imp{{ID: "1"}},
	}

	_, err := request.ToDeviceRecord()
	if err != ErrMissingIFA {
		t.Fatalf("ToDeviceRecord() error = %v, want %v", err, ErrMissingIFA)
	}
}

func TestToDeviceRecordAllowsIOSWithoutIFA(t *testing.T) {
	request := BidRequest{
		App:    App{Bundle: "com.example.app"},
		Device: Device{OS: "iOS", IP: "2001:db8::1", UA: "Mozilla/5.0 (iPhone)"},
		Imp:    []Imp{{ID: "1"}},
	}

	record, err := request.ToDeviceRecord()
	if err != nil {
		t.Fatalf("ToDeviceRecord() error = %v", err)
	}
	if record.OS != "ios" || record.IFA != "" {
		t.Fatalf("unexpected iOS mapping: %#v", record)
	}
}
