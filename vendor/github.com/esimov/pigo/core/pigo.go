package pigo

import (
	"bytes"
	"encoding/binary"
	"math"
	"sort"
	"unsafe"
)

// CascadeParams contains the basic parameters to run the analyzer function over the defined image.
// MinSize: represents the minimum size of the face.
// MaxSize: represents the maximum size of the face.
// ShiftFactor: determines to what percentage to move the detection window over its size.
// ScaleFactor: defines in percentage the resize value of the detection window when moving to a higher scale.
type CascadeParams struct {
	MinSize     int
	MaxSize     int
	ShiftFactor float64
	ScaleFactor float64
}

// ImageParams is a struct for image related settings.
// Pixels: contains the grayscale converted image pixel data.
// Rows: the number of image rows.
// Cols: the number of image columns.
// Dim: the image dimension.
type ImageParams struct {
	Pixels []uint8
	Rows   int
	Cols   int
	Dim    int
}

// Pigo struct defines the basic binary tree components.
type Pigo struct {
	treeDepth     uint32
	treeNum       uint32
	treeCodes     []int8
	treePred      []float32
	treeThreshold []float32
}

// NewPigo instantiate a new pigo struct.
func NewPigo() *Pigo {
	return &Pigo{}
}

// Unpack unpack the binary face classification file.
func (pg *Pigo) Unpack(packet []byte) (*Pigo, error) {
	var (
		treeDepth     uint32
		treeNum       uint32
		treeCodes     []int8
		treePred      []float32
		treeThreshold []float32
	)

	// We skip the first 8 bytes of the cascade file.
	pos := 8
	buff := make([]byte, 4)
	dataView := bytes.NewBuffer(buff)

	// Read the depth (size) of each tree and write it into the buffer array.
	_, err := dataView.Write([]byte{packet[pos+0], packet[pos+1], packet[pos+2], packet[pos+3]})
	if err != nil {
		return nil, err
	}

	if dataView.Len() > 0 {
		treeDepth = binary.LittleEndian.Uint32(packet[pos:])
		pos += 4

		// Get the number of cascade trees as 32-bit unsigned integer and write it into the buffer array.
		_, err := dataView.Write([]byte{packet[pos+0], packet[pos+1], packet[pos+2], packet[pos+3]})
		if err != nil {
			return nil, err
		}

		treeNum = binary.LittleEndian.Uint32(packet[pos:])
		pos += 4

		for t := 0; t < int(treeNum); t++ {
			treeCodes = append(treeCodes, []int8{0, 0, 0, 0}...)

			code := packet[pos : pos+int(4*math.Pow(2, float64(treeDepth))-4)]
			// Convert unsigned bytecodes to signed ones.
			signedCode := *(*[]int8)(unsafe.Pointer(&code))
			treeCodes = append(treeCodes, signedCode...)

			pos = pos + int(4*math.Pow(2, float64(treeDepth))-4)

			// Read prediction from tree's leaf nodes.
			for i := 0; i < int(math.Pow(2, float64(treeDepth))); i++ {
				_, err := dataView.Write([]byte{packet[pos+0], packet[pos+1], packet[pos+2], packet[pos+3]})
				if err != nil {
					return nil, err
				}
				u32pred := binary.LittleEndian.Uint32(packet[pos:])
				// Convert uint32 to float32
				f32pred := *(*float32)(unsafe.Pointer(&u32pred))
				treePred = append(treePred, f32pred)
				pos += 4
			}

			// Read tree nodes threshold values.
			_, err := dataView.Write([]byte{packet[pos+0], packet[pos+1], packet[pos+2], packet[pos+3]})
			if err != nil {
				return nil, err
			}
			u32thr := binary.LittleEndian.Uint32(packet[pos:])
			// Convert uint32 to float32
			f32thr := *(*float32)(unsafe.Pointer(&u32thr))
			treeThreshold = append(treeThreshold, f32thr)
			pos += 4
		}
	}
	return &Pigo{
		treeDepth,
		treeNum,
		treeCodes,
		treePred,
		treeThreshold,
	}, nil
}

// classifyRegion constructs the classification function based on the parsed binary data.
func (pg *Pigo) classifyRegion(r, c, s int, pixels []uint8, dim int) float32 {
	var (
		root  int = 0
		out   float32
		pTree = int(math.Pow(2, float64(pg.treeDepth)))
	)

	r = r * 256
	c = c * 256

	for i := 0; i < int(pg.treeNum); i++ {
		var idx = 1

		for j := 0; j < int(pg.treeDepth); j++ {
			var pix = 0
			var x1 = ((r+int(pg.treeCodes[root+4*idx+0])*s)>>8)*dim + ((c + int(pg.treeCodes[root+4*idx+1])*s) >> 8)
			var x2 = ((r+int(pg.treeCodes[root+4*idx+2])*s)>>8)*dim + ((c + int(pg.treeCodes[root+4*idx+3])*s) >> 8)

			var px1 = pixels[x1]
			var px2 = pixels[x2]

			if px1 <= px2 {
				pix = 1
			} else {
				pix = 0
			}
			idx = 2*idx + pix
		}
		out += pg.treePred[pTree*i+idx-pTree]

		if out <= pg.treeThreshold[i] {
			return -1.0
		} else {
			root += 4 * pTree
		}
	}
	return out - pg.treeThreshold[pg.treeNum-1]
}

// Detection struct contains the detection results composed of
// the row, column, scale factor and the detection score.
type Detection struct {
	Row   int
	Col   int
	Scale int
	Q     float32
}

// RunCascade analyze the grayscale converted image pixel data and run the classification function over the detection window.
// It will return a slice containing the detection row, column, it's center and the detection score (in case this is > than 0.0).
func (pg *Pigo) RunCascade(img ImageParams, opts CascadeParams) []Detection {
	var detections []Detection
	var pixels = img.Pixels

	scale := opts.MinSize

	// Run the classification function over the detection window
	// and check if the false positive rate is above a certain value.
	for scale <= opts.MaxSize {
		step := int(math.Max(opts.ShiftFactor*float64(scale), 1))
		offset := (scale/2 + 1)

		for row := offset; row <= img.Rows-offset; row += step {
			for col := offset; col <= img.Cols-offset; col += step {
				q := pg.classifyRegion(row, col, scale, pixels, img.Dim)
				if q > 0.0 {
					detections = append(detections, Detection{row, col, scale, q})
				}
			}
		}
		scale = int(float64(scale) * opts.ScaleFactor)
	}
	return detections
}

// ClusterDetections returns the intersection over union of multiple clusters.
// We need to make this comparision to filter out multiple face detection regions.
func (pg *Pigo) ClusterDetections(detections []Detection, iouThreshold float64) []Detection {
	// Sort detections by their score
	sort.Sort(det(detections))

	calcIoU := func(det1, det2 Detection) float64 {
		// Unpack the position and size of each detection.
		r1, c1, s1 := float64(det1.Row), float64(det1.Col), float64(det1.Scale)
		r2, c2, s2 := float64(det2.Row), float64(det2.Col), float64(det2.Scale)

		overRow := math.Max(0, math.Min(r1+s1/2, r2+s2/2)-math.Max(r1-s1/2, r2-s2/2))
		overCol := math.Max(0, math.Min(c1+s1/2, c2+s2/2)-math.Max(c1-s1/2, c2-s2/2))

		// Return intersection over union.
		return overRow * overCol / (s1*s1 + s2*s2 - overRow*overCol)
	}
	assignments := make([]bool, len(detections))
	clusters := []Detection{}

	for i := 0; i < len(detections); i++ {
		// Compare the intersection over union only for two different clusters.
		// Skip the comparison in case there already exists a cluster A in the bucket.
		if !assignments[i] {
			var (
				r, c, s, n int
				q          float32
			)
			for j := 0; j < len(detections); j++ {
				// Check if the comparision result is below a certain threshold.
				if calcIoU(detections[i], detections[j]) > iouThreshold {
					assignments[j] = true
					r += detections[j].Row
					c += detections[j].Col
					s += detections[j].Scale
					q += detections[j].Q
					n++
				}
			}
			if n > 0 {
				clusters = append(clusters, Detection{r / n, c / n, s / n, q})
			}
		}
	}
	return clusters
}

// Implement sorting function on detection values.
type det []Detection

func (q det) Len() int      { return len(q) }
func (q det) Swap(i, j int) { q[i], q[j] = q[j], q[i] }
func (q det) Less(i, j int) bool {
	if q[i].Q < q[j].Q {
		return true
	}
	if q[i].Q > q[j].Q {
		return false
	}
	return q[i].Q < q[j].Q
}
