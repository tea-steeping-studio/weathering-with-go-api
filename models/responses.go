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
// CurrentWeatherPayload, ForecastPayload and SevenDayPayload, the last three
// types in this file, are the decode-side types among them: pointer-shaped views of
// the same bodies, kept beside the upstream structs they complement and the schemas
// they feed. Their own doc comments say why they have to exist.

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

// RainBlock represents the rain block of the /data/2.5 endpoints. OneHour is a
// documented window, so it carries no omitempty: an absent reading reports null
// and a block upstream reported with no window in it does not look like a block
// that was never sent. ThreeHour is the window /data/2.5/weather does not
// document, so it is tagged omitempty, which on a pointer field drops only nil and
// can never discard a measured zero. /data/2.5/forecast does not document OneHour
// at all, so it uses ForecastRainBlock instead of this type.
type RainBlock struct {
	OneHour   *float64 `json:"1h"`
	ThreeHour *float64 `json:"3h,omitempty"`
}

// SnowBlock represents the snow block of the /data/2.5 endpoints. It tags its
// members for the same reasons as RainBlock: OneHour has no omitempty because the
// window is documented, and ThreeHour has it because this endpoint does not
// document that window. /data/2.5/forecast uses ForecastSnowBlock.
type SnowBlock struct {
	OneHour   *float64 `json:"1h"`
	ThreeHour *float64 `json:"3h,omitempty"`
}

// ForecastRainBlock is the rain block of the /data/2.5/forecast slots and of the
// day rolled up from them.
//
// It exists because the two forecast endpoints need opposite treatment of the
// same two windows and one shared type cannot express both. /data/2.5/weather
// documents 1h and not 3h, so RainBlock makes 1h a required key that reports null
// when the upstream measured nothing: a body of "rain":{} has to read as
// {"1h":null} rather than as {}. /data/2.5/forecast documents 3h and not 1h, so
// the 1h key must be absent entirely rather than permanently null, and a key the
// endpoint does not document carries no information. Omitting the member is the
// only way to say that with a struct tag.
//
// ThreeHour is a pointer with no omitempty, because the window is documented here:
// a measured 0 serialises as 0, a block upstream sent with no window in it reports
// null, and a block upstream never sent is a nil block, which is three different
// readings.
type ForecastRainBlock struct {
	ThreeHour *float64 `json:"3h"`
}

// ForecastSnowBlock is the snow block of the /data/2.5/forecast route, and exists
// for exactly the reason ForecastRainBlock does: the 1h window is not documented
// here, so the member is absent rather than null, while 3h is documented and
// reports null, 0 or an absent block as three separate states.
type ForecastSnowBlock struct {
	ThreeHour *float64 `json:"3h"`
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
// timezone is a pointer to match the pointer-shaped payload it is read from, so
// the faithful schema states one nullability rule rather than two.
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
// and the authority on which members the upstream actually sent. It declares the
// conditional members the mapper cannot recover from OpenWeatherMapResponse: the
// main, wind, clouds and sys blocks as pointers, main.sea_level,
// main.grnd_level, main.temp_kf, wind.gust, the rain and snow blocks with their
// 1h windows, and timezone.
//
// It exists beside OpenWeatherMapResponse because that struct is a non-pointer
// decode target, where a member the upstream measured as 0 and a member it never
// sent are the same zero. A response that has to report the difference cannot be
// built from it, so the body is decoded twice from the same already-fetched bytes:
// the mapper takes values from the upstream struct and presence from here. A newly
// documented upstream field has to be added to both types, because a field missing
// from this one is reported as null even when the upstream did send it.
// TestCurrentPayloadCoversDecodedFields is the guard on that, and its allowlist
// names every JSON member this type deliberately narrows.
//
// Precipitation is taken from here and not from owm: models.Rain and models.Snow
// are still decoded on every fetch and read by nobody, because their non-pointer
// windows cannot tell a measured zero from an absent one. Do not read them, and do
// not add a third copy of them here.
//
// timezone is bound here and nowhere else. OpenWeatherMapResponse does not carry
// it because no legacy field is derived from it, and an unused decode field is
// worse than a missing one.
type CurrentWeatherPayload struct {
	Main *currentPayloadMain `json:"main"`
	Wind *currentPayloadWind `json:"wind"`
	// Clouds and Sys carry no members on purpose. Every member they report is
	// unconditionally sent, so the mapper reads those values from
	// OpenWeatherMapResponse and needs only the pointer to know the block was
	// there at all.
	Clouds *struct{}             `json:"clouds"`
	Sys    *struct{}             `json:"sys"`
	Rain   *currentPayloadPrecip `json:"rain"`
	Snow   *currentPayloadPrecip `json:"snow"`
	// Timezone is the UTC offset the upstream reports, in seconds.
	Timezone *int `json:"timezone"`
}

// currentPayloadMain holds the main block members that are conditional rather
// than guaranteed: sea_level and grnd_level come only from points near sea level
// or the ground, and temp_kf is not always sent. The rest of main is read from
// OpenWeatherMapResponse.
type currentPayloadMain struct {
	SeaLevel  *int     `json:"sea_level"`
	GrndLevel *int     `json:"grnd_level"`
	TempKF    *float64 `json:"temp_kf"`
}

// currentPayloadWind holds wind.gust, which some regions report and others omit.
// speed and deg are read from OpenWeatherMapResponse.
type currentPayloadWind struct {
	Gust *float64 `json:"gust"`
}

// currentPayloadPrecip holds the 1h window both rain and snow report on this
// endpoint. A nil block means upstream sent no block at all, which is not the
// same as a block whose window measures 0. The 3h window is not declared here
// because /data/2.5/weather does not document it; see RainBlock.
type currentPayloadPrecip struct {
	OneHour *float64 `json:"1h"`
}

// TempPoint is a time of day temperature breakdown. /data/3.0/onecall fills every
// member it documents. /data/2.5/forecast has no breakdown at all: it fills only
// Min and Max there, and those two are derived by the mapper from the day's slots
// rather than measured by the upstream. The rest stay null, because a value the
// endpoint cannot supply is not a zero.
type TempPoint struct {
	Day   *float64 `json:"day"`
	Min   *float64 `json:"min"`
	Max   *float64 `json:"max"`
	Night *float64 `json:"night"`
	Morn  *float64 `json:"morn"`
	Eve   *float64 `json:"eve"`
}

// FeelsLikePoint is the apparent temperature breakdown. Every member is null on
// /forecast, which reports no breakdown; /forecast/7day fills all four.
type FeelsLikePoint struct {
	Day   *float64 `json:"day"`
	Night *float64 `json:"night"`
	Morn  *float64 `json:"morn"`
	Eve   *float64 `json:"eve"`
}

// ForecastSlot is one raw three hour slot from the list[] array, carried on the day
// it belongs to so a client can read a single period instead of a daily rollup.
// The members the upstream did not send are null, so a slot that measures a zero
// and a slot that was never told about the quantity are different readings.
//
// Sys is the one place a response type here points at an upstream struct rather
// than a pointer shaped one: pod is a string the endpoint sends whenever the sys
// block is there, so the pointer on this field is the whole of the presence
// signal, and a second pointer for a string the endpoint does document would add
// nothing.
//
// Rain and Snow use the forecast precipitation types rather than the shared ones,
// so the 1h window is absent here rather than null: this endpoint documents the
// 3h window only, and a key it does not document carries no information. See
// ForecastRainBlock for why the two routes cannot share one type.
type ForecastSlot struct {
	Dt         int64              `json:"dt"`
	Main       *MainBlock         `json:"main"`
	Weather    []Weather          `json:"weather"`
	Clouds     *CloudsBlock       `json:"clouds"`
	Wind       *WindBlock         `json:"wind"`
	Visibility *int               `json:"visibility"`
	Pop        *float64           `json:"pop"`
	Rain       *ForecastRainBlock `json:"rain"`
	Snow       *ForecastSnowBlock `json:"snow"`
	Sys        *ForecastSys       `json:"sys"`
	DtTxt      string             `json:"dt_txt"`
}

// ForecastDay is one entry of the forecast route's forecast[] array, in the
// upstream field order of /data/2.5/forecast rolled up to a day, carrying the
// legacy vocabulary alongside it.
//
// Three JSON keys are wanted by both vocabularies and two Go fields cannot share
// one tag, so one field serves each pair. Date is the day the slots fell on.
// Humidity is the day's mean, which is what legacy Forecast.Humidity has always
// been. WindSpeed is the day's mean, which is what legacy Forecast.WindSpeed has
// always been. Their JSON is byte identical to the response this type replaces.
//
// Every numeric key here is a pointer, precipitation excepted and explained below,
// because the route cannot always produce one: a day whose slots reported no main
// block has no temperature to report, and three fabricated zeroes beside a null temp
// block is worse than saying so. For a real body the JSON is byte identical, because
// a non-nil pointer to a value serialises as that value.
//
// ChanceOfRain is null when no slot carried pop, UVIndex is always null because the
// three hour endpoint reports no uvi, and Pressure is null when no slot carried a
// main block.
type ForecastDay struct {
	Date        time.Time `json:"date"`
	MaxTemp     *float64  `json:"max_temperature"`
	MinTemp     *float64  `json:"min_temperature"`
	AvgTemp     *float64  `json:"avg_temperature"`
	Condition   string    `json:"condition"`
	Description string    `json:"description"`
	Icon        string    `json:"icon"`
	// Humidity truncates its daily mean while Pressure, Clouds and Visibility round
	// theirs, and the split is deliberate. Humidity's legacy counterpart is an int
	// field that has always truncated, so rounding it would change a number existing
	// consumers already read and buy no accuracy: a mean of 67.5 percent is 67 either
	// way as far as a legacy int key is concerned. The other three have no legacy
	// counterpart, so nothing constrains them and they carry the nearest integer.
	//
	// Precipitation is a pointer, and the rule is the seven day route's rule rather
	// than a near miss at it. A total is an answer only when there was something to
	// total: a day with one slot reporting a 3h rain window the upstream measured as 0
	// has a total of 0, and a day with no slot reporting any window has no total at all.
	// As a value both reported 0, and the second one sat beside a rain and a snow that
	// were both null, so the response said nothing fell and measured nothing at the same
	// time.
	Humidity      *int     `json:"humidity"`
	WindSpeed     *float64 `json:"wind_speed"`
	Precipitation *float64 `json:"precipitation"`
	ChanceOfRain  *int     `json:"chance_of_rain"`
	UVIndex       *float64 `json:"uv_index"`

	// Pop is the highest probability any slot of the day reported, PopMin the
	// lowest and PopMean the mean of the slots that carried one. All three are null
	// when no slot reported a probability, which is a different statement from a day
	// whose probability was measured as zero.
	//
	// Temp.Min and Temp.Max are derived: the extremes of the day's slot
	// temperatures. They are not the same quantity as a slot's main.temp_min and
	// main.temp_max, which the upstream measures over its own three hour window.
	//
	// Clouds, Visibility, Pressure, WindDeg, WindGust, Rain and Snow are the day's
	// rollup as well. WindDeg is the direction of the slot with the day's strongest
	// wind, matching the documented meaning of deg on the daily endpoints. Rain and
	// Snow are sums of the slots' 3h windows and are null when no slot reported one.
	//
	// FeelsLike and Uvi are always null here: the three hour endpoint reports
	// neither.
	Pop        *float64           `json:"pop"`
	PopMin     *float64           `json:"pop_min"`
	PopMean    *float64           `json:"pop_mean"`
	Temp       *TempPoint         `json:"temp"`
	FeelsLike  *FeelsLikePoint    `json:"feels_like"`
	WindDeg    *int               `json:"wind_deg"`
	WindGust   *float64           `json:"wind_gust"`
	Clouds     *int               `json:"clouds"`
	Visibility *int               `json:"visibility"`
	Pressure   *int               `json:"pressure"`
	Rain       *ForecastRainBlock `json:"rain"`
	Snow       *ForecastSnowBlock `json:"snow"`
	Uvi        *float64           `json:"uvi"`
	Weather    []Weather          `json:"weather"`
	Hourly     []ForecastSlot     `json:"hourly"`
}

// ForecastResponse is the body of GET|POST /api/v1/weather/forecast, served inside
// the {"success":true,"data":{...}} envelope.
//
// The first group mirrors the /data/2.5/forecast envelope, in upstream field order.
// cod is a string on this endpoint and message is the float the upstream sends, the
// calculation time, so neither is an int. cnt is the integer the decode struct
// binds. None of their zeroes is a reading a real response makes, so all three are
// value fields. City is a pointer because the block can be absent.
//
// The second group is the legacy vocabulary, unchanged, so existing consumers keep
// working. Forecast is a slice with no omitempty, so the key is always present even
// when the upstream sent no slots.
type ForecastResponse struct {
	Cod     string  `json:"cod"`
	Message float64 `json:"message"`
	Cnt     int     `json:"cnt"`
	City    *City   `json:"city"`

	Location    Location      `json:"location"`
	Current     Current       `json:"current"`
	Forecast    []ForecastDay `json:"forecast"`
	RequestTime time.Time     `json:"request_time"`
}

// ForecastPayload is a pointer-shaped mirror of the /data/2.5/forecast body and the
// authority on which members the upstream actually sent. It declares the
// conditional members the mapper cannot recover from
// OpenWeatherMapForecastResponse: per slot, the main, wind, clouds, rain, snow and
// sys blocks as pointers, main.sea_level, main.grnd_level, wind.gust, the rain and
// snow 3h windows, and pop and visibility; and at the envelope level, the city
// block.
//
// It exists beside OpenWeatherMapForecastResponse because models.ForecastItem and
// the blocks it holds are non-pointer decode targets, where a member the upstream
// measured as 0 and a member it never sent are the same zero. A response that has
// to report the difference cannot be built from them, so the body is decoded twice
// from the same already-fetched bytes: the mapper takes values from the upstream
// struct and presence from here. That is one upstream request, not two. A newly
// documented upstream field has to be added to both types, because a field missing
// from this one is reported as absent even when the upstream did send it.
// TestForecastPayloadCoversDecodedFields is the guard on that, and its allowlist
// names every JSON member this type deliberately narrows.
//
// The two list[] decodes see the same array in the same order, so index i of List
// is the presence view of index i of OpenWeatherMapForecastResponse.List. A body
// whose list is longer than the payload's cannot happen: both decodes walk one
// array, and a member one of them cannot type fails the request rather than
// shortening it.
//
// pop is treated as conditional because the endpoint does not send it on every
// slot, and the daily rollup excludes the slots that carry none from pop, pop_min
// and pop_mean alike.
type ForecastPayload struct {
	// City carries no members on purpose. Every member the block reports is
	// unconditionally sent, so the mapper reads those values from the upstream
	// struct and needs only the pointer to know the block was there at all.
	City *struct{}             `json:"city"`
	List []ForecastPayloadItem `json:"list"`
}

// ForecastPayloadItem is the presence view of one three hour slot. Its members are
// the slots' conditional ones; dt, dt_txt, weather, the always sent members of
// main, wind and clouds, and sys.pod are read from the upstream struct.
type ForecastPayloadItem struct {
	Main       *ForecastPayloadMain   `json:"main"`
	Wind       *ForecastPayloadWind   `json:"wind"`
	Clouds     *struct{}              `json:"clouds"`
	Rain       *ForecastPayloadPrecip `json:"rain"`
	Snow       *ForecastPayloadPrecip `json:"snow"`
	Sys        *struct{}              `json:"sys"`
	Pop        *float64               `json:"pop"`
	Visibility *int                   `json:"visibility"`
}

// ForecastPayloadMain holds the main block members of a slot that are conditional
// rather than guaranteed: sea_level and grnd_level come only from points near sea
// level or the ground. temp_kf is not declared here because /data/2.5/forecast does
// not document it for a slot, so the response reports it null.
type ForecastPayloadMain struct {
	SeaLevel  *int `json:"sea_level"`
	GrndLevel *int `json:"grnd_level"`
}

// ForecastPayloadWind holds wind.gust on a slot, which some regions report and
// others omit. speed and deg are read from the upstream struct.
type ForecastPayloadWind struct {
	Gust *float64 `json:"gust"`
}

// ForecastPayloadPrecip holds the 3h window both rain and snow report on this
// endpoint. A nil block means the upstream sent no block at all, which is not the
// same as a block whose window measures 0. The 1h window is not declared here
// because /data/2.5/forecast does not document it; see RainBlock.
type ForecastPayloadPrecip struct {
	ThreeHour *float64 `json:"3h"`
}

// OneCallCurrentPoint is the faithful current block of /data/3.0/onecall, in
// upstream field order. Every member is a pointer, because the decode struct this
// is read beside is a non-pointer target where a member the upstream measured as 0
// and a member it never sent are the same zero.
//
// The block documents no probability, so there is no pop here to copy from
// /forecast's: minutely[].precipitation is a probability and current is not. The
// legacy current block on this route reports max_temperature and min_temperature as
// null, because this block carries no extremes of its own; the day carries them, as a
// time of day breakdown. Those two legacy keys were a fabricated 0 until models.Current
// became pointer shaped, and the 0 was the only reading either route ever had for
// them.
type OneCallCurrentPoint struct {
	Dt         *int64    `json:"dt"`
	Sunrise    *int64    `json:"sunrise"`
	Sunset     *int64    `json:"sunset"`
	Temp       *float64  `json:"temp"`
	FeelsLike  *float64  `json:"feels_like"`
	Pressure   *int      `json:"pressure"`
	Humidity   *int      `json:"humidity"`
	DewPoint   *float64  `json:"dew_point"`
	Uvi        *float64  `json:"uvi"`
	Clouds     *int      `json:"clouds"`
	Visibility *int      `json:"visibility"`
	WindSpeed  *float64  `json:"wind_speed"`
	WindDeg    *int      `json:"wind_deg"`
	WindGust   *float64  `json:"wind_gust"`
	Weather    []Weather `json:"weather"`
}

// OneCallDailyPoint is one entry of the faithful daily[] array, in the upstream
// field order of /data/3.0/onecall. It is uncapped: the array is whatever the
// upstream sent, which is a different thing from the legacy array this route also
// reports, capped at seven days.
//
// Temp and FeelsLike reuse the shared breakdown types rather than declaring their
// own. /onecall is the endpoint that fills every member of both, so the nullability
// rule is the shared one: a member the upstream sent as 0 is 0 and a member it never
// sent is null. A block the upstream did not send is a nil block, which is a third
// state distinct from both.
//
// Rain and Snow are plain volumes in millimetres and are not the rain and snow
// blocks of the /data/2.5 endpoints, so they take neither RainBlock nor SnowBlock.
// Those two types describe a {1h} or {3h} window inside a block; this endpoint
// reports a total for the day and has no window to describe.
type OneCallDailyPoint struct {
	Dt        *int64          `json:"dt"`
	Sunrise   *int64          `json:"sunrise"`
	Sunset    *int64          `json:"sunset"`
	Moonrise  *int64          `json:"moonrise"`
	Moonset   *int64          `json:"moonset"`
	MoonPhase *float64        `json:"moon_phase"`
	Temp      *TempPoint      `json:"temp"`
	FeelsLike *FeelsLikePoint `json:"feels_like"`
	Pressure  *int            `json:"pressure"`
	Humidity  *int            `json:"humidity"`
	DewPoint  *float64        `json:"dew_point"`
	WindSpeed *float64        `json:"wind_speed"`
	WindDeg   *int            `json:"wind_deg"`
	WindGust  *float64        `json:"wind_gust"`
	Weather   []Weather       `json:"weather"`
	Clouds    *int            `json:"clouds"`
	Pop       *float64        `json:"pop"`
	Rain      *float64        `json:"rain"`
	Snow      *float64        `json:"snow"`
	Uvi       *float64        `json:"uvi"`
}

// OneCallEnvelope is the faithful /data/3.0/onecall body, namespaced under one key.
//
// The namespace is not cosmetic. The legacy current block and the faithful One Call
// current object both want the JSON key current, and two Go fields cannot share one
// tag, so grouping the faithful data under onecall removes the collision and leaves
// the legacy location, current, forecast and request_time keys exactly where they
// were. The three opt-in arrays live at onecall.minutely, onecall.hourly and
// onecall.alerts.
//
// The envelope is always present, so a caller who asks for no opt-in block still
// receives lat, lon, timezone, timezone_offset, current and daily.
//
// The three opt-in members are pointers to slices with omitempty, which on a pointer
// drops only nil. That is what separates the three states a caller has to tell apart:
// a block that was never exposed is a nil pointer and the key is absent; a block the
// caller asked for that the upstream reported as [] is a pointer to an empty slice and
// the key is there with an empty array in it; and a block the caller asked for with
// entries is a pointer to them. A plain slice with omitempty cannot do that, because
// it drops a non-nil empty slice as readily as a nil one, so a caller who asked for
// alerts against the very common "alerts":[] body would be told nothing at all.
//
// Clearing a block is therefore a matter of nil-ing the pointer, and it has to happen
// on a copy: the cached response is shared by every concurrent caller and must never
// be trimmed in place. The projection that does so is not written yet.
//
// The four scalars are values. The upstream documents all of them as sent whenever
// the endpoint answers, and a latitude of 0 and a timezone offset of 0 are both real
// readings at the places that have them.
//
// Daily has no omitempty, so the key is there even when the upstream sent no day at
// all, in which case it is null. Tags has none either, so an alert the upstream sent
// with no tag list reports a null array and one it sent with an empty list reports an
// empty array.
type OneCallEnvelope struct {
	Lat            float64                 `json:"lat"`
	Lon            float64                 `json:"lon"`
	Timezone       string                  `json:"timezone"`
	TimezoneOffset int                     `json:"timezone_offset"`
	Current        *OneCallCurrentPoint    `json:"current"`
	Daily          []OneCallDailyPoint     `json:"daily"`
	Minutely       *[]OneCallMinutelyPoint `json:"minutely,omitempty"`
	Hourly         *[]OneCallHourlyPoint   `json:"hourly,omitempty"`
	Alerts         *[]OneCallAlertPoint    `json:"alerts,omitempty"`
}

// OneCallMinutelyPoint is one entry of the faithful minutely array.
//
// Precipitation is a probability over the minute, not a volume of rain, and this
// endpoint is the only place that reports it as one: the hourly and daily blocks
// carry volumes. Reading it as a volume would be a different number entirely, so the
// member is named for what the upstream documents and the doc says what it is.
type OneCallMinutelyPoint struct {
	Dt            *int64   `json:"dt"`
	Precipitation *float64 `json:"precipitation"`
}

// OneCallHourlyPoint is one entry of the faithful hourly array, in the upstream field
// order.
//
// Rain and Snow are volumes in millimetres over the hour and are plain floats on this
// endpoint, exactly as they are on the daily entry, so they take neither RainBlock
// nor SnowBlock: those describe a {1h} or {3h} window inside a block and this
// endpoint has no window to describe. It omits them for an hour nothing fell on, so
// a measured 0 and an absent member are two different readings and every member here
// is a pointer.
type OneCallHourlyPoint struct {
	Dt         *int64    `json:"dt"`
	Sunrise    *int64    `json:"sunrise"`
	Sunset     *int64    `json:"sunset"`
	Temp       *float64  `json:"temp"`
	FeelsLike  *float64  `json:"feels_like"`
	Pressure   *int      `json:"pressure"`
	Humidity   *int      `json:"humidity"`
	DewPoint   *float64  `json:"dew_point"`
	Uvi        *float64  `json:"uvi"`
	Clouds     *int      `json:"clouds"`
	Visibility *int      `json:"visibility"`
	WindSpeed  *float64  `json:"wind_speed"`
	WindDeg    *int      `json:"wind_deg"`
	WindGust   *float64  `json:"wind_gust"`
	Pop        *float64  `json:"pop"`
	Rain       *float64  `json:"rain"`
	Snow       *float64  `json:"snow"`
	Weather    []Weather `json:"weather"`
}

// OneCallAlertPoint is one entry of the faithful alerts array: a government weather
// alert covering a span of time.
//
// Every member is a pointer, so an alert the upstream sent with an empty sender name
// reports "" and one that sent no sender name at all reports null. Those are
// different statements, and a government alert with no sender is worth telling apart
// from one whose sender is the empty string. Tags is a slice and not a pointer: nil
// means the upstream sent no list and [] means it sent an empty one, which is the
// same distinction with a slice.
type OneCallAlertPoint struct {
	SenderName  *string  `json:"sender_name"`
	Event       *string  `json:"event"`
	Start       *int64   `json:"start"`
	End         *int64   `json:"end"`
	Description *string  `json:"description"`
	Tags        []string `json:"tags"`
}

// SevenDayResponse is the body of GET|POST /api/v1/weather/forecast/7day, served
// inside the {"success":true,"data":{...}} envelope.
//
// The first group is the faithful mirror of /data/3.0/onecall, under one key; see
// OneCallEnvelope for why it is namespaced. The second group is the legacy
// vocabulary, unchanged, so existing consumers keep working. Forecast is a slice
// with no omitempty, where the envelope this type replaces had one, so the key is
// always present: an upstream with no daily entry now reports "forecast":[] rather
// than no key at all.
type SevenDayResponse struct {
	OneCall *OneCallEnvelope `json:"onecall"`

	Location    Location   `json:"location"`
	Current     Current    `json:"current"`
	Forecast    []Forecast `json:"forecast"`
	RequestTime time.Time  `json:"request_time"`
}

// SevenDayPayload is a pointer-shaped mirror of the /data/3.0/onecall body and the
// authority on which members the upstream actually sent.
//
// It exists beside OneCallResponse because that struct and every type it holds are
// non-pointer decode targets, where a member the upstream measured as 0 and a member
// it never sent are the same zero. A response that has to report the difference
// cannot be built from them, so the body is decoded twice from the same
// already-fetched bytes: the mapper takes values from the upstream struct where the
// upstream documents the member as sent, and reads both value and presence from here
// for everything else. That is one upstream request, not two. A newly documented
// upstream field has to be added to both types, because a field missing from this one
// is reported as null even when the upstream did send it.
// TestSevenDayPayloadCoversDecodedFields is the guard on that, and its allowlist
// names every JSON member this type deliberately narrows.
//
// The rule for what is declared here is two questions asked of each member: can the
// upstream omit it for reasons of its own, and does anything the response reports
// derive from it? A member that fails both is read from the upstream struct and
// allowlisted, because its zero is a real reading. A member that passes either is
// declared, so an absent one is null rather than a fabricated number. That is why
// humidity and wind_speed are declared on a day and pressure and clouds are not: the
// first two are the readings the legacy array is built from, so an absent one has to
// be able to reach that array as a null.
//
// The three opt-in arrays are declared as the response point types, not as parallel
// presence structs. Nothing legacy reads them, so there is no non-pointer decode
// struct to supplement and no second copy to keep in step: a field added to
// OneCallHourlyPoint and forgotten here would not compile, which is the whole failure
// mode a duplicated pair invites. Their values therefore come from here directly.
//
// The current block and the daily array are the other case, and the reason those two
// do need a parallel view: OneCallResponse and the types it holds are non-pointer
// decode targets, so a member the upstream measured as 0 and a member it never sent
// are the same zero, and a presence view is the only way to tell them apart.
//
// Every block the response reports is pointer shaped, so no block on this route is a
// place where a measured zero is indistinguishable from an absent member. Only the
// members the allowlist names are read from the decode struct, and each of those is a
// claim about the upstream rather than a type choice.
//
// The daily array is walked by index: index i of Daily is the presence view of index
// i of OneCallResponse.Daily. Both are decodes of one body, so they line up; a body
// whose array is longer than this one's cannot happen, and a member either of them
// cannot type fails the request rather than shortening it.
type SevenDayPayload struct {
	Current *SevenDayPayloadCurrent `json:"current"`
	Daily   []SevenDayPayloadDaily  `json:"daily"`
	// The three opt-in arrays are the response types. A nil member means the upstream
	// sent no such key, which is what the envelope reports as an absent block; an
	// empty non-nil member means it sent [] and the envelope reports an empty array.
	// That distinction is the reason they are decoded here rather than anywhere else.
	Minutely []OneCallMinutelyPoint `json:"minutely"`
	Hourly   []OneCallHourlyPoint   `json:"hourly"`
	Alerts   []OneCallAlertPoint    `json:"alerts"`
}

// SevenDayPayloadCurrent declares the current block's measurements. The timestamps
// and the weather array are read from the upstream struct, where a zero is a real
// reading and an absent array is already reported as null by the response type.
type SevenDayPayloadCurrent struct {
	Temp       *float64 `json:"temp"`
	FeelsLike  *float64 `json:"feels_like"`
	Pressure   *int     `json:"pressure"`
	Humidity   *int     `json:"humidity"`
	DewPoint   *float64 `json:"dew_point"`
	Uvi        *float64 `json:"uvi"`
	Clouds     *int     `json:"clouds"`
	Visibility *int     `json:"visibility"`
	WindSpeed  *float64 `json:"wind_speed"`
	WindDeg    *int     `json:"wind_deg"`
	WindGust   *float64 `json:"wind_gust"`
}

// SevenDayPayloadDaily declares the measurements on one daily entry.
//
// Temp and FeelsLike carry no members on purpose. The six and four members inside
// those blocks are documented as unconditionally sent, so the mapper reads those
// values from the upstream struct and needs only the pointer to know the block was
// there at all. The eleven measurements below are read from here, value and
// presence together, so the response and the legacy array cannot report a reading
// this body did not carry.
//
// Rain and Snow are volumes in millimetres. A day nothing fell on carries neither,
// which is a different reading from a day the upstream measured a volume of 0.
type SevenDayPayloadDaily struct {
	Temp      *struct{} `json:"temp"`
	FeelsLike *struct{} `json:"feels_like"`
	Humidity  *int      `json:"humidity"`
	DewPoint  *float64  `json:"dew_point"`
	WindSpeed *float64  `json:"wind_speed"`
	WindDeg   *int      `json:"wind_deg"`
	WindGust  *float64  `json:"wind_gust"`
	Pop       *float64  `json:"pop"`
	Rain      *float64  `json:"rain"`
	Snow      *float64  `json:"snow"`
	Uvi       *float64  `json:"uvi"`
}
