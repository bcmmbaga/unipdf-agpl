/*
 * This file is subject to the terms and conditions defined in
 * file 'LICENSE.md', which is part of this source code package.
 */

package optimize

import (
	"github.com/bcmmbaga/unipdf-agpl/v3/core"
)

// CompressStreams compresses uncompressed streams.
// It implements interface model.Optimizer.
type CompressStreams struct {
}

// Optimize optimizes PDF objects to decrease PDF size.
func (c *CompressStreams) Optimize(objects []core.PdfObject) (optimizedObjects []core.PdfObject, err error) {
	optimizedObjects = make([]core.PdfObject, len(objects))
	copy(optimizedObjects, objects)
	for _, obj := range objects {
		stream, isStreamObj := core.GetStream(obj)
		if !isStreamObj {
			continue
		}
		// Skip objects that are already encoded.
		// TODO: Try filter combinations, and ignoring inefficient filters.
		if obj := stream.Get("Filter"); obj != nil {
			if _, skip := core.GetName(obj); skip {
				continue
			}
			if arr, ok := core.GetArray(obj); ok && arr.Len() > 0 {
				continue
			}
		}

		encoder := core.NewFlateEncoder() // Most mainstream compressor and probably most robust.
		var data []byte
		data, err = encoder.EncodeBytes(stream.Stream)
		if err != nil {
			return optimizedObjects, err
		}
		dict := encoder.MakeStreamDict()
		// compare compressed and uncompressed sizes
		if len(data)+len(dict.WriteString()) < len(stream.Stream) {
			stream.Stream = data
			stream.PdfObjectDictionary.Merge(dict)
			stream.PdfObjectDictionary.Set("Length", core.MakeInteger(int64(len(stream.Stream))))
		}
	}
	return optimizedObjects, nil
}
