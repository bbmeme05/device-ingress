package openrtb

import (
	"bytes"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
)

var (
	ErrMissingDevice      = errors.New("missing device")
	ErrUnsupportedOS      = errors.New("unsupported device os")
	ErrMissingIP          = errors.New("missing or invalid device ip")
	ErrMissingUA          = errors.New("missing device ua")
	ErrMissingAppID       = errors.New("missing app bundle or id")
	ErrMissingIFA         = errors.New("missing or invalid android ifa")
	ErrMissingImpression  = errors.New("missing impression")
	validAndroidIFARegexp = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
)

// BidRequest contains only fields needed to create the existing device-filter input.
// OpenRTB ext fields deliberately remain unparsed for forward compatibility.
type BidRequest struct {
	ID       string     `json:"id"`
	App      App        `json:"app"`
	Device   Device     `json:"device"`
	Imp      []Imp      `json:"imp"`
	Source   Source     `json:"source"`
	ClientID FlexibleID `json:"clientId"`
	SupplyID FlexibleID `json:"supplyId"`
}

type App struct {
	ID         string    `json:"id"`
	Bundle     string    `json:"bundle"`
	Name       string    `json:"name"`
	Categories []string  `json:"cat"`
	Publisher  Publisher `json:"publisher"`
}

type Publisher struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Device struct {
	OS             string `json:"os"`
	IP             string `json:"ip"`
	UA             string `json:"ua"`
	IFA            string `json:"ifa"`
	Language       string `json:"language"`
	Make           string `json:"make"`
	Model          string `json:"model"`
	OSVersion      string `json:"osv"`
	Carrier        string `json:"carrier"`
	ConnectionType int    `json:"connectiontype"`
	Geo            Geo    `json:"geo"`
}

type Geo struct {
	Country string `json:"country"`
	City    string `json:"city"`
	Region  string `json:"region"`
}

type Imp struct {
	ID     string `json:"id"`
	TagID  string `json:"tagid"`
	Banner Banner `json:"banner"`
}

type Banner struct {
	Width  int `json:"w"`
	Height int `json:"h"`
}

type Source struct {
	TransactionID string `json:"tid"`
}

// FlexibleID accepts numeric or string exchange-specific identifiers.
type FlexibleID string

func (id *FlexibleID) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*id = ""
		return nil
	}
	if data[0] == '"' {
		value, err := strconv.Unquote(string(data))
		if err != nil {
			return err
		}
		*id = FlexibleID(value)
		return nil
	}
	if _, err := strconv.ParseFloat(string(data), 64); err != nil {
		return fmt.Errorf("identifier must be string or number: %w", err)
	}
	*id = FlexibleID(string(data))
	return nil
}

// DeviceRecord is the JSON schema consumed by the deployed Java device-filter.
type DeviceRecord struct {
	OS             string `json:"os"`
	Country        string `json:"country,omitempty"`
	AdxID          string `json:"adx_id,omitempty"`
	IP             string `json:"ip"`
	UA             string `json:"ua"`
	Device         string `json:"device,omitempty"`
	Brand          string `json:"brand,omitempty"`
	IFA            string `json:"ifa,omitempty"`
	AppID          string `json:"app_id"`
	Cat            string `json:"cat,omitempty"`
	SessionID      string `json:"session_id,omitempty"`
	AdHeight       string `json:"ad_height,omitempty"`
	AdPlacement    string `json:"ad_placement,omitempty"`
	AdRequestID    string `json:"ad_request_id,omitempty"`
	AdType         string `json:"ad_type,omitempty"`
	AdWidth        string `json:"ad_width,omitempty"`
	Channel        string `json:"channel,omitempty"`
	City           string `json:"city,omitempty"`
	NetworkAccess  string `json:"network_access,omitempty"`
	NetworkCarrier string `json:"network_carrier,omitempty"`
	OSVersion      string `json:"os_version,omitempty"`
	SiteID         string `json:"site_id,omitempty"`
	State          string `json:"state,omitempty"`
	MediaSource    string `json:"media_source,omitempty"`
}

func (r BidRequest) ToDeviceRecord() (DeviceRecord, error) {
	if len(r.Imp) == 0 {
		return DeviceRecord{}, ErrMissingImpression
	}
	if r.Device.OS == "" {
		return DeviceRecord{}, ErrMissingDevice
	}

	os := strings.ToLower(strings.TrimSpace(r.Device.OS))
	if os != "android" && os != "ios" {
		return DeviceRecord{}, ErrUnsupportedOS
	}

	ip := strings.TrimSpace(r.Device.IP)
	if ip == "" {
		return DeviceRecord{}, ErrMissingIP
	}
	if _, err := netip.ParseAddr(ip); err != nil {
		return DeviceRecord{}, ErrMissingIP
	}

	ua := strings.TrimSpace(r.Device.UA)
	if ua == "" {
		return DeviceRecord{}, ErrMissingUA
	}

	appID := strings.TrimSpace(r.App.Bundle)
	if appID == "" {
		appID = strings.TrimSpace(r.App.ID)
	}
	if appID == "" {
		return DeviceRecord{}, ErrMissingAppID
	}

	ifa := strings.ToLower(strings.TrimSpace(r.Device.IFA))
	if os == "android" && !validAndroidIFARegexp.MatchString(ifa) {
		return DeviceRecord{}, ErrMissingIFA
	}

	imp := r.Imp[0]
	adxID := string(r.ClientID)
	if adxID == "" {
		adxID = string(r.SupplyID)
	}

	return DeviceRecord{
		OS:             os,
		Country:        strings.ToUpper(strings.TrimSpace(r.Device.Geo.Country)),
		AdxID:          adxID,
		IP:             ip,
		UA:             ua,
		Device:         strings.TrimSpace(r.Device.Model),
		Brand:          strings.TrimSpace(r.Device.Make),
		IFA:            ifa,
		AppID:          appID,
		Cat:            strings.Join(r.App.Categories, ","),
		SessionID:      strings.TrimSpace(r.Source.TransactionID),
		AdHeight:       positiveIntString(imp.Banner.Height),
		AdPlacement:    strings.TrimSpace(imp.TagID),
		AdRequestID:    strings.TrimSpace(r.ID),
		AdType:         "banner",
		AdWidth:        positiveIntString(imp.Banner.Width),
		Channel:        strings.TrimSpace(r.App.Publisher.ID),
		City:           strings.TrimSpace(r.Device.Geo.City),
		NetworkAccess:  strconv.Itoa(r.Device.ConnectionType),
		NetworkCarrier: strings.TrimSpace(r.Device.Carrier),
		OSVersion:      strings.TrimSpace(r.Device.OSVersion),
		SiteID:         strings.TrimSpace(imp.ID),
		State:          strings.TrimSpace(r.Device.Geo.Region),
		MediaSource:    string(r.SupplyID),
	}, nil
}

func positiveIntString(value int) string {
	if value <= 0 {
		return ""
	}
	return strconv.Itoa(value)
}
