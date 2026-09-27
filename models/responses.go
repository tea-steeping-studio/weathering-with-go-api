package models

import "time"

// The block types below describe what a route reports, not what it decodes. Every
// member is a pointer, because a value the upstream genuinely measured as 0 has
// to serialise as 0, and only a member the upstream had no value for may
// serialise as null. Most members carry no omitempty. A pointer member may carry
// omitempty safely, since it then drops only nil and can never discard a measured
// zero; the precipitation blocks use it so that a window the route's endpoint does
// not document is omitted instead of reported as null.
//
// A block that is itself absent is nil, which is a third state distinct from
// both: a block of nulls would claim the upstream reported the block and found
// every member missing.
//
// The upstream structs in openweather.go cannot serve this purpose. They are
// decode targets whose zero values are correct, and several of them carry
// omitempty, which drops a measured 0 on the floor.
//
// CurrentWeatherPayload, the last type in this file, is the one decode-side type
// among them: a pointer-shaped view of the same body, kept beside the upstream
// struct it complements and the schema it feeds. Its own doc comment says why it
// has to exist.

// MainBlock represents the main block of the /data/2.5 endpoints. sea_level and
// grnd_level are reported only for points near sea level or the ground, so they
// are null everywhere else.
type MainBlock struct {
	Temp      *float64 `json:"temp"`
	FeelsLike *float64 `json:"feels_like"`
	TempMin   *float64 `json:"temp_min"`
	TempMax   *float64 `json:"temp_max"`
	Pressure  *int     `json:"pressure"`
	Humidity  *int     `json:"humidity"`
	SeaLevel  *int     `json:"sea_level"`
	GrndLevel *int     `json:"grnd_level"`
	TempKF    *float64 `json:"temp_kf"`
}

// WindBlock represents the wind block of the /data/2.5 endpoints. gust is
// reported by some regions and not by others.
type WindBlock struct {
	Speed *float64 `json:"speed"`
	Deg   *int     `json:"deg"`
	Gust  *float64 `json:"gust"`
}

// CloudsBlock represents the clouds block of the /data/2.5 endpoints.
type CloudsBlock struct {
	All *int `json:"all"`
}

// RainBlock represents the rain block of the /data/2.5 endpoints. Both members
// are pointers, so their omitempty drops only a window the upstream did not send
// and cannot discard a measured zero: a window that measures 0 still serialises
// as 0. The window the route's endpoint does not document is omitted entirely,
// because /data/2.5/weather reports 1h and /data/2.5/forecast reports 3h.
type RainBlock struct {
	OneHour   *float64 `json:"1h,omitempty"`
	ThreeHour *float64 `json:"3h,omitempty"`
}

// SnowBlock represents the snow block of the /data/2.5 endpoints. It tags its
// members omitempty for the same reason as RainBlock: the fields are pointers, so
// only an absent window is dropped and a measured zero still serialises as 0.
type SnowBlock struct {
	OneHour   *float64 `json:"1h,omitempty"`
	ThreeHour *float64 `json:"3h,omitempty"`
}

// SysBlock represents the sys block of the /data/2.5 endpoints.
type SysBlock struct {
	Type    *int    `json:"type"`
	ID      *int    `json:"id"`
	Country *string `json:"country"`
	Sunrise *int64  `json:"sunrise"`
	Sunset  *int64  `json:"sunset"`
}

// CurrentWeatherResponse is the body of GET|POST /api/v1/weather/current, served
// inside the {"success":true,"data":{...}} envelope.
//
// The first group is a faithful mirror of the /data/2.5/weather schema, in
// upstream field order. coord and weather reuse the upstream types, which carry
// no omitempty and are fully populated whenever that endpoint answers at all.
// visibility, dt, id, cod, base and name are always sent by that endpoint, so
// they are plain values: none of their zeroes is a reading a real response makes.
// timezone is a pointer because models.OpenWeatherMapResponse has no field for
// it; the service binds it from the payload, so it is null only if the upstream
// omits it.
//
// The second group is the legacy vocabulary, unchanged, so existing consumers keep
// working. The legacy blocks keep their own omitempty, which means a legacy key
// whose value is zero can still be absent while its faithful twin reports the
// zero.
//
// /data/2.5/weather has no pop, so no chance_of_rain key is emitted here in either
// vocabulary. The legacy Current struct has never carried one, and a key that
// could only ever be null would carry no information.
type CurrentWeatherResponse struct {
	Coord      Coordinates  `json:"coord"`
	Weather    []Weather    `json:"weather"`
	Base       string       `json:"base"`
	Main       *MainBlock   `json:"main"`
	Visibility int          `json:"visibility"`
	Wind       *WindBlock   `json:"wind"`
	Clouds     *CloudsBlock `json:"clouds"`
	Rain       *RainBlock   `json:"rain"`
	Snow       *SnowBlock   `json:"snow"`
	Dt         int64        `json:"dt"`
	Sys        *SysBlock    `json:"sys"`
	ID         int          `json:"id"`
	Timezone   *int         `json:"timezone"`
	Name       string       `json:"name"`
	Cod        int          `json:"cod"`

	Location    Location  `json:"location"`
	Current     Current   `json:"current"`
	RequestTime time.Time `json:"request_time"`
}

// CurrentWeatherPayload is a pointer-shaped mirror of the /data/2.5/weather body
// and the authority on which members the upstream actually sent. Only the members
// whose presence cannot be recovered from OpenWeatherMapResponse are declared
// here: main.sea_level, main.grnd_level, main.temp_kf, wind.gust, the rain and
// snow blocks with their 1h windows, and timezone.
//
// It exists beside OpenWeatherMapResponse because that struct is a non-pointer
// decode target, where a member the upstream measured as 0 and a member it never
// sent are the same zero. A response that has to report the difference cannot be
// built from it, so the body is decoded twice from the same already-fetched bytes:
// the mapper takes values from the upstream struct and presence from here. A newly
// documented upstream field has to be added to both types, because a field missing
// from this one is reported as null even when the upstream did send it.
//
// timezone is bound here and nowhere else. OpenWeatherMapResponse does not carry
// it because no legacy field is derived from it, and an unused decode field is
// worse than a missing one.
type CurrentWeatherPayload struct {
	Main currentPayloadMain    `json:"main"`
	Wind currentPayloadWind    `json:"wind"`
	Rain *currentPayloadPrecip `json:"rain"`
	Snow *currentPayloadPrecip `json:"snow"`
	// Timezone is the UTC offset the upstream reports in seconds. It is a pointer
	// so a response reports null rather than a fabricated zero if it is absent.
	Timezone *int `json:"timezone"`
}

// currentPayloadMain holds the main block members that are conditional rather
// than guaranteed: sea_level and grnd_level come only from points near sea level
// or the ground, and temp_kf is not always sent.
type currentPayloadMain struct {
	SeaLevel  *int     `json:"sea_level"`
	GrndLevel *int     `json:"grnd_level"`
	TempKF    *float64 `json:"temp_kf"`
}

// currentPayloadWind holds wind.gust, which some regions report and others omit.
type currentPayloadWind struct {
	Gust *float64 `json:"gust"`
}

// currentPayloadPrecip holds the 1h window both rain and snow report on this
// endpoint. A nil block means upstream sent no block at all, which is not the
// same as a block whose window measures 0.
type currentPayloadPrecip struct {
	OneHour *float64 `json:"1h"`
}
