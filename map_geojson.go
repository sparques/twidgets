package twidgets

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"math"

	"github.com/gdamore/tcell/v2"
)

//go:embed continents.geojson
var continentsGeojson []byte

//go:embed countries.geojson
var countriesGeojson []byte

//go:embed us-states.geojson
var usstatesGeojson []byte

//go:embed us-counties.geojson
var uscountiesGeojson []byte

type GeoJSONOptions struct {
	// If true, polygon rings are emitted as closed paths.
	// This should usually be true.
	ClosePolygons bool

	// Optional transform applied while loading.
	// Handy if you later want to project or pre-transform data.
	Transform func(Position) Position
}

func DefaultGeoJSONOptions() GeoJSONOptions {
	return GeoJSONOptions{
		ClosePolygons: true,
	}
}

func LoadGeoJSONPaths(r io.Reader, opts GeoJSONOptions) ([]*Path, error) {
	if opts.Transform == nil {
		opts.Transform = func(p Position) Position { return p }
	}

	var raw json.RawMessage
	dec := json.NewDecoder(r)
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}

	return parseGeoJSONValue(raw, opts)
}

func (m *Map) AddGeoJSON(r io.Reader, opts GeoJSONOptions) error {
	paths, err := LoadGeoJSONPaths(r, opts)
	if err != nil {
		return err
	}
	m.Paths = append(m.Paths, paths...)
	return nil
}

// LoadEarth loads PathLayers containing geospatial data for countries, us-states, and us-counties.
func (m *Map) LoadEarth() *Map {

	continentsPaths, _ := LoadGeoJSONPaths(bytes.NewBuffer(continentsGeojson), DefaultGeoJSONOptions())
	countriesPaths, _ := LoadGeoJSONPaths(bytes.NewBuffer(countriesGeojson), DefaultGeoJSONOptions())
	usStatesPaths, _ := LoadGeoJSONPaths(bytes.NewBuffer(usstatesGeojson), DefaultGeoJSONOptions())
	usCountiesPaths, _ := LoadGeoJSONPaths(bytes.NewBuffer(uscountiesGeojson), DefaultGeoJSONOptions())

	continents := NewPathLayer("countinents", continentsPaths).
		WithPriority(4).
		WithScaleRange(0, 0.5).
		SetStyle(tcell.StyleDefault.Foreground(tcell.Color239))

	countries := NewPathLayer("countries", countriesPaths).
		WithPriority(3).
		WithScaleRange(0.5, 100).
		SetStyle(tcell.StyleDefault.Foreground(tcell.Color239))

	usStates := NewPathLayer("states", usStatesPaths).
		WithPriority(2).
		WithScaleRange(1, 100).
		SetStyle(tcell.StyleDefault.Foreground(tcell.Color236))

	usCounties := NewPathLayer("counties", usCountiesPaths).
		WithPriority(1).
		WithScaleRange(7, 1000).
		SetStyle(tcell.StyleDefault.Foreground(tcell.Color234))

	m.AddPathProvider(continents)
	m.AddPathProvider(countries)
	m.AddPathProvider(usStates)
	m.AddPathProvider(usCounties)

	return m
}

type geoJSONTypeProbe struct {
	Type string `json:"type"`
}

type geoJSONFeatureCollection struct {
	Type     string            `json:"type"`
	Features []json.RawMessage `json:"features"`
}

type geoJSONFeature struct {
	Type       string          `json:"type"`
	Geometry   json.RawMessage `json:"geometry"`
	Properties json.RawMessage `json:"properties"`
	ID         json.RawMessage `json:"id"`
}

type geoJSONGeometry struct {
	Type        string            `json:"type"`
	Coordinates json.RawMessage   `json:"coordinates"`
	Geometries  []json.RawMessage `json:"geometries"`
}

func parseGeoJSONValue(raw json.RawMessage, opts GeoJSONOptions) ([]*Path, error) {
	var probe geoJSONTypeProbe
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, fmt.Errorf("probe geojson type: %w", err)
	}

	switch probe.Type {
	case "FeatureCollection":
		var fc geoJSONFeatureCollection
		if err := json.Unmarshal(raw, &fc); err != nil {
			return nil, fmt.Errorf("decode FeatureCollection: %w", err)
		}
		var out []*Path
		for i, featRaw := range fc.Features {
			paths, err := parseGeoJSONValue(featRaw, opts)
			if err != nil {
				return nil, fmt.Errorf("feature %d: %w", i, err)
			}
			out = append(out, paths...)
		}
		return out, nil

	case "Feature":
		var f geoJSONFeature
		if err := json.Unmarshal(raw, &f); err != nil {
			return nil, fmt.Errorf("decode Feature: %w", err)
		}
		if len(f.Geometry) == 0 || string(f.Geometry) == "null" {
			return nil, nil
		}
		return parseGeoJSONValue(f.Geometry, opts)

	case "Polygon", "MultiPolygon", "LineString", "MultiLineString", "GeometryCollection":
		var g geoJSONGeometry
		if err := json.Unmarshal(raw, &g); err != nil {
			return nil, fmt.Errorf("decode Geometry: %w", err)
		}
		return parseGeoJSONGeometry(g, opts)

	default:
		return nil, fmt.Errorf("unsupported geojson type %q", probe.Type)
	}
}

func parseGeoJSONGeometry(g geoJSONGeometry, opts GeoJSONOptions) ([]*Path, error) {
	switch g.Type {
	case "LineString":
		var coords [][]float64
		if err := json.Unmarshal(g.Coordinates, &coords); err != nil {
			return nil, fmt.Errorf("decode LineString coordinates: %w", err)
		}
		p, err := lineStringToPath(coords, false, opts)
		if err != nil {
			return nil, err
		}
		if p == nil {
			return nil, nil
		}
		return []*Path{p}, nil

	case "MultiLineString":
		var coords [][][]float64
		if err := json.Unmarshal(g.Coordinates, &coords); err != nil {
			return nil, fmt.Errorf("decode MultiLineString coordinates: %w", err)
		}
		var out []*Path
		for i, line := range coords {
			p, err := lineStringToPath(line, false, opts)
			if err != nil {
				return nil, fmt.Errorf("multilinestring line %d: %w", i, err)
			}
			if p != nil {
				out = append(out, p)
			}
		}
		return out, nil

	case "Polygon":
		// Polygon = array of linear rings.
		// First ring is exterior, subsequent rings are holes.
		// Since your Path model has no fill/hole semantics, we emit every ring as its own closed path.
		var coords [][][]float64
		if err := json.Unmarshal(g.Coordinates, &coords); err != nil {
			return nil, fmt.Errorf("decode Polygon coordinates: %w", err)
		}
		var out []*Path
		for i, ring := range coords {
			p, err := lineStringToPath(ring, opts.ClosePolygons, opts)
			if err != nil {
				return nil, fmt.Errorf("polygon ring %d: %w", i, err)
			}
			if p != nil {
				out = append(out, p)
			}
		}
		return out, nil

	case "MultiPolygon":
		var coords [][][][]float64
		if err := json.Unmarshal(g.Coordinates, &coords); err != nil {
			return nil, fmt.Errorf("decode MultiPolygon coordinates: %w", err)
		}
		var out []*Path
		for pi, poly := range coords {
			for ri, ring := range poly {
				p, err := lineStringToPath(ring, opts.ClosePolygons, opts)
				if err != nil {
					return nil, fmt.Errorf("multipolygon polygon %d ring %d: %w", pi, ri, err)
				}
				if p != nil {
					out = append(out, p)
				}
			}
		}
		return out, nil

	case "GeometryCollection":
		var out []*Path
		for i, subRaw := range g.Geometries {
			var sub geoJSONGeometry
			if err := json.Unmarshal(subRaw, &sub); err != nil {
				return nil, fmt.Errorf("geometrycollection geometry %d: %w", i, err)
			}
			paths, err := parseGeoJSONGeometry(sub, opts)
			if err != nil {
				return nil, fmt.Errorf("geometrycollection geometry %d: %w", i, err)
			}
			out = append(out, paths...)
		}
		return out, nil

	default:
		return nil, fmt.Errorf("unsupported geometry type %q", g.Type)
	}
}

func lineStringToPath(coords [][]float64, closed bool, opts GeoJSONOptions) (*Path, error) {
	if len(coords) == 0 {
		return nil, nil
	}

	pts := make([]Position, 0, len(coords))
	for i, c := range coords {
		p, err := coordToPosition(c)
		if err != nil {
			return nil, fmt.Errorf("coordinate %d: %w", i, err)
		}
		pts = append(pts, opts.Transform(p))
	}

	// GeoJSON polygon rings often repeat the first point as the last point.
	// If we're marking the path closed, that final duplicate is redundant for your renderer.
	if closed && len(pts) >= 2 {
		first := pts[0]
		last := pts[len(pts)-1]
		if positionsEqual(first, last) {
			pts = pts[:len(pts)-1]
		}
	}

	if len(pts) < 2 {
		return nil, nil
	}

	return &Path{
		Positions: pts,
		IsClosed:  closed,
	}, nil
}

func coordToPosition(c []float64) (Position, error) {
	// GeoJSON positions are [lon, lat, (optional elevation...)]
	if len(c) < 2 {
		return Position{}, fmt.Errorf("need at least 2 numbers, got %d", len(c))
	}
	return Position{
		X: c[0],
		Y: c[1],
	}, nil
}

func positionsEqual(a, b Position) bool {
	return a.X == b.X && a.Y == b.Y
}

const earthRadiusKm = 6371.0

func LatLonDistance(a, b Position) (kilometers float64) {
	// Convert degrees to radians
	lat1 := a.Y * math.Pi / 180
	lon1 := a.X * math.Pi / 180
	lat2 := b.Y * math.Pi / 180
	lon2 := b.X * math.Pi / 180

	dlat := lat2 - lat1
	dlon := lon2 - lon1

	sinDLat := math.Sin(dlat / 2)
	sinDLon := math.Sin(dlon / 2)

	h := sinDLat*sinDLat +
		math.Cos(lat1)*math.Cos(lat2)*sinDLon*sinDLon

	return 2 * earthRadiusKm * math.Asin(math.Sqrt(h))
}
