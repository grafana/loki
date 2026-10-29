package parquet

import (
	"math"
	"slices"

	ethrift "github.com/parquet-go/parquet-go/encoding/thrift"
	"github.com/parquet-go/parquet-go/format"
	geom "github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/wkb"
)

// geospatialBBoxAccumulator accumulates a bounding box across all non-null WKB
// values written to a GEOMETRY or GEOGRAPHY column within one row group.
type geospatialBBoxAccumulator struct {
	hasValues    bool // true once a non-empty geometry extends the bbox
	hasGeomTypes bool // true once any non-null geometry (including empty) is seen
	parseError   bool // true if any WKB value failed to parse; suppresses stats output
	xMin, xMax   float64
	yMin, yMax   float64
	hasZ         bool
	zMin, zMax   float64
	hasM         bool
	mMin, mMax   float64
	geomTypeSet  map[int32]struct{}
}

func newGeospatialBBoxAccumulator() *geospatialBBoxAccumulator {
	a := &geospatialBBoxAccumulator{}
	a.reset()
	return a
}

func (a *geospatialBBoxAccumulator) reset() {
	a.hasValues = false
	a.hasGeomTypes = false
	a.parseError = false
	a.xMin, a.xMax = math.Inf(1), math.Inf(-1)
	a.yMin, a.yMax = math.Inf(1), math.Inf(-1)
	a.hasZ = false
	a.zMin, a.zMax = math.Inf(1), math.Inf(-1)
	a.hasM = false
	a.mMin, a.mMax = math.Inf(1), math.Inf(-1)
	a.geomTypeSet = make(map[int32]struct{})
}

func (a *geospatialBBoxAccumulator) updateFromGeom(g geom.T) {
	if g == nil {
		return
	}

	// Always record the type code: empty geometries are still non-null values
	// with a defined WKB type and must appear in GeoSpatialTypes.
	if code := wkbGeomTypeCode(g); code != 0 {
		a.geomTypeSet[code] = struct{}{}
		a.hasGeomTypes = true
	}

	bounds := g.Bounds()
	if bounds == nil || bounds.IsEmpty() {
		return
	}

	xMin := bounds.Min(0)
	xMax := bounds.Max(0)
	yMin := bounds.Min(1)
	yMax := bounds.Max(1)

	if !math.IsNaN(xMin) && !math.IsNaN(xMax) {
		if !a.hasValues || xMin < a.xMin {
			a.xMin = xMin
		}
		if !a.hasValues || xMax > a.xMax {
			a.xMax = xMax
		}
	}
	if !math.IsNaN(yMin) && !math.IsNaN(yMax) {
		if !a.hasValues || yMin < a.yMin {
			a.yMin = yMin
		}
		if !a.hasValues || yMax > a.yMax {
			a.yMax = yMax
		}
	}

	layout := g.Layout()
	if zIdx := layout.ZIndex(); zIdx >= 0 {
		zMin := bounds.Min(zIdx)
		zMax := bounds.Max(zIdx)
		if !math.IsNaN(zMin) && !math.IsNaN(zMax) {
			if !a.hasZ || zMin < a.zMin {
				a.zMin = zMin
			}
			if !a.hasZ || zMax > a.zMax {
				a.zMax = zMax
			}
			a.hasZ = true
		}
	}
	if mIdx := layout.MIndex(); mIdx >= 0 {
		mMin := bounds.Min(mIdx)
		mMax := bounds.Max(mIdx)
		if !math.IsNaN(mMin) && !math.IsNaN(mMax) {
			if !a.hasM || mMin < a.mMin {
				a.mMin = mMin
			}
			if !a.hasM || mMax > a.mMax {
				a.mMax = mMax
			}
			a.hasM = true
		}
	}

	a.hasValues = true
}

func (a *geospatialBBoxAccumulator) accumulatePage(page Page) {
	reader := page.Values()
	var buf [64]Value
	for {
		n, err := reader.ReadValues(buf[:])
		for _, v := range buf[:n] {
			if v.IsNull() {
				continue
			}
			g, parseErr := wkb.Unmarshal(v.ByteArray())
			if parseErr != nil {
				// A non-null value that cannot be parsed makes the stats
				// unreliable for this chunk; suppress them entirely.
				a.parseError = true
				return
			}
			a.updateFromGeom(g)
		}
		if err != nil {
			break
		}
	}
}

func (a *geospatialBBoxAccumulator) toGeospatialStatistics() format.GeospatialStatistics {
	// Suppress stats entirely if a WKB parse error occurred: the accumulated
	// data would describe only a subset of the column chunk values.
	if a.parseError || !a.hasGeomTypes {
		return format.GeospatialStatistics{}
	}

	geomTypes := make([]int32, 0, len(a.geomTypeSet))
	for t := range a.geomTypeSet {
		geomTypes = append(geomTypes, t)
	}
	slices.Sort(geomTypes)

	stats := format.GeospatialStatistics{
		GeoSpatialTypes: geomTypes,
	}

	// BBox is only meaningful when at least one non-empty geometry was seen.
	if a.hasValues {
		bbox := format.BoundingBox{
			XMin: a.xMin,
			XMax: a.xMax,
			YMin: a.yMin,
			YMax: a.yMax,
		}
		if a.hasZ {
			bbox.ZMin = ethrift.New(a.zMin)
			bbox.ZMax = ethrift.New(a.zMax)
		}
		if a.hasM {
			bbox.MMin = ethrift.New(a.mMin)
			bbox.MMax = ethrift.New(a.mMax)
		}
		stats.BBox = bbox
	}

	return stats
}

// wkbGeomTypeCode returns the OGC WKB geometry type code for g.
// Base codes: 1=Point, 2=LineString, 3=Polygon, 4=MultiPoint,
// 5=MultiLineString, 6=MultiPolygon, 7=GeometryCollection.
// Z variants add 1000, M variants add 2000, ZM variants add 3000.
func wkbGeomTypeCode(g geom.T) int32 {
	var base int32
	switch g.(type) {
	case *geom.Point:
		base = 1
	case *geom.LineString:
		base = 2
	case *geom.Polygon:
		base = 3
	case *geom.MultiPoint:
		base = 4
	case *geom.MultiLineString:
		base = 5
	case *geom.MultiPolygon:
		base = 6
	case *geom.GeometryCollection:
		base = 7
	default:
		return 0
	}
	switch g.Layout() {
	case geom.XYZ:
		return base + 1000
	case geom.XYM:
		return base + 2000
	case geom.XYZM:
		return base + 3000
	}
	return base
}
