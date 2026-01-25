package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// OneCallResponse represents the OpenWeatherMap One Call 3.0 API response.
type OneCallResponse struct {
	Current struct {
		Temp      float64 `json:"temp"`
		FeelsLike float64 `json:"feels_like"`
		Humidity  int     `json:"humidity"`
		WindSpeed float64 `json:"wind_speed"`
		Weather   []struct {
			ID          int    `json:"id"`
			Main        string `json:"main"`
			Description string `json:"description"`
			Icon        string `json:"icon"`
		} `json:"weather"`
	} `json:"current"`
	Minutely []struct {
		Dt            int64   `json:"dt"`            // Unix timestamp
		Precipitation float64 `json:"precipitation"` // mm/h
	} `json:"minutely"`
	Daily []struct {
		Temp struct {
			Min float64 `json:"min"`
			Max float64 `json:"max"`
		} `json:"temp"`
		Weather []struct {
			ID          int    `json:"id"`
			Main        string `json:"main"`
			Description string `json:"description"`
			Icon        string `json:"icon"`
		} `json:"weather"`
	} `json:"daily"`
}

// CurrentWeather holds current weather conditions.
type CurrentWeather struct {
	Temp        float64
	FeelsLike   float64
	Humidity    int
	WindSpeed   float64
	Condition   string // Main condition (Clear, Clouds, Rain, etc.)
	Description string // Detailed description
	Icon        string // Icon code (01d, 02n, etc.)
}

// DailyForecast holds today's forecast.
type DailyForecast struct {
	TempMin   float64
	TempMax   float64
	Condition string
	Icon      string
}

// PrecipForecast holds precipitation forecast info.
type PrecipForecast struct {
	Active      bool   // Currently precipitating
	StartsIn    int    // Minutes until precip starts (0 if already active or none expected)
	EndsIn      int    // Minutes until precip ends (0 if not active or won't end in forecast)
	Type        string // "Rain", "Snow", "Sleet", etc.
	Description string // Human-readable description
}

// fetchOneCall fetches weather data from the One Call 3.0 API.
func fetchOneCall(ctx context.Context, apiKey string, lat, lon float64) (CurrentWeather, DailyForecast, PrecipForecast, error) {
	baseURL := "https://api.openweathermap.org/data/3.0/onecall"

	params := url.Values{}
	params.Set("lat", fmt.Sprintf("%.6f", lat))
	params.Set("lon", fmt.Sprintf("%.6f", lon))
	params.Set("appid", apiKey)
	params.Set("units", "imperial")
	params.Set("exclude", "hourly,alerts")

	reqURL := baseURL + "?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return CurrentWeather{}, DailyForecast{}, PrecipForecast{}, fmt.Errorf("create request: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return CurrentWeather{}, DailyForecast{}, PrecipForecast{}, fmt.Errorf("fetch weather: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return CurrentWeather{}, DailyForecast{}, PrecipForecast{}, fmt.Errorf("API error: %s", resp.Status)
	}

	var data OneCallResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return CurrentWeather{}, DailyForecast{}, PrecipForecast{}, fmt.Errorf("decode response: %w", err)
	}

	current := CurrentWeather{
		Temp:      data.Current.Temp,
		FeelsLike: data.Current.FeelsLike,
		Humidity:  data.Current.Humidity,
		WindSpeed: data.Current.WindSpeed,
	}

	if len(data.Current.Weather) > 0 {
		current.Condition = data.Current.Weather[0].Main
		current.Description = data.Current.Weather[0].Description
		current.Icon = data.Current.Weather[0].Icon
	}

	var daily DailyForecast
	if len(data.Daily) > 0 {
		daily.TempMin = data.Daily[0].Temp.Min
		daily.TempMax = data.Daily[0].Temp.Max
		if len(data.Daily[0].Weather) > 0 {
			daily.Condition = data.Daily[0].Weather[0].Main
			daily.Icon = data.Daily[0].Weather[0].Icon
		}
	}

	precip := analyzePrecipitation(data.Minutely, current.Condition)

	return current, daily, precip, nil
}

// analyzePrecipitation analyzes minutely data to determine precipitation status.
func analyzePrecipitation(minutely []struct {
	Dt            int64   `json:"dt"`
	Precipitation float64 `json:"precipitation"`
}, condition string) PrecipForecast {
	if len(minutely) == 0 {
		return PrecipForecast{}
	}

	// Determine precipitation type from current condition
	precipType := getPrecipType(condition)

	const threshold = 0.1 // mm/h threshold to consider it precipitating

	isActive := minutely[0].Precipitation >= threshold

	var forecast PrecipForecast
	forecast.Active = isActive
	forecast.Type = precipType

	if isActive {
		// Find when precip ends
		for i, m := range minutely {
			if m.Precipitation < threshold {
				forecast.EndsIn = i
				forecast.Description = fmt.Sprintf("%s ending in %d min", precipType, i)
				break
			}
		}
		if forecast.EndsIn == 0 {
			forecast.Description = fmt.Sprintf("%s for 60+ min", precipType)
		}
	} else {
		// Find when precip starts
		for i, m := range minutely {
			if m.Precipitation >= threshold {
				forecast.StartsIn = i
				forecast.Description = fmt.Sprintf("%s in %d min", precipType, i)
				break
			}
		}
		// No description if no precip expected
	}

	return forecast
}

// getPrecipType determines precipitation type from condition string.
func getPrecipType(condition string) string {
	switch condition {
	case "Snow":
		return "Snow"
	case "Sleet":
		return "Sleet"
	case "Thunderstorm":
		return "Storm"
	case "Drizzle":
		return "Drizzle"
	default:
		return "Rain"
	}
}
