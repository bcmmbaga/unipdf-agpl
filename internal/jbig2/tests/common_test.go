/*
 * This file is subject to the terms and conditions defined in
 * file 'LICENSE.md', which is part of this source code package.
 */

package tests

import (
	"archive/zip"
	"crypto/md5"
	"encoding/hex"
	"flag"
	"fmt"
	_ "image/gif"
	"image/jpeg"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/bcmmbaga/unipdf-agpl/v3/common"
	"github.com/bcmmbaga/unipdf-agpl/v3/contentstream"
	"github.com/bcmmbaga/unipdf-agpl/v3/core"
	"github.com/bcmmbaga/unipdf-agpl/v3/model"

	"github.com/bcmmbaga/unipdf-agpl/v3/internal/jbig2/document"
)

// register basic image drivers - gif, jpeg, png

const (
	// EnvJBIG2Directory is the environment variable that should contain directory path
	// to the jbig2 encoded test files.
	EnvJBIG2Directory = "UNIDOC_JBIG2_TESTDATA"
	// EnvImageDirectory is the environment variable that should contain directory path
	// to the images used for the encoding processes.
	EnvImageDirectory = "UNIDOC_JBIG2_TEST_IMAGES"
)

var (
	// updateGoldens is the runtime flag that states that the md5 hashes
	// for each decoded test case image should be updated.
	updateGoldens bool
	// keepImageFiles is the runtime flag that is used to keep the decoded jbig2 images
	// within the temporary directory: 'os.TempDir()/unipdf/jbig2'.
	keepImageFiles bool
	// logToFile is the flag that allows to log the trace values to the file.
	logToFile bool
	// keepEncodedFile is the runtime flag that is used to keep the jbig2 encoded images
	// within the temporary directory: 'os.TempDir()/unipdf/jbig2'
	keepEncodedFile bool
)

func init() {
	flag.BoolVar(&updateGoldens, "jbig2-update-goldens", false, "updates the golden file hashes on the run")
	flag.BoolVar(&keepImageFiles, "jbig2-store-images", false, "stores the images in the temporary `os.TempDir`/unipdf/jbig2 directory")
	flag.BoolVar(&keepEncodedFile, "jbig2-store-encoded", false, "stores the jbig2 encoded images in the temporary `os.TempDir`/unipdf/jbig2 directory")
	flag.BoolVar(&logToFile, "jbig2-log-to-file", false, "logs trace messages into file localized at `os.TempDir`/unipdf/jbig2 directory")
}

type extractedImage struct {
	jbig2Data []byte
	pdfImage  *model.XObjectImage
	name      string
	pageNo    int
	idx       int
	hash      string
	globals   *document.Globals
}

func (e *extractedImage) fullName() string {
	return fmt.Sprintf("%s_%d_%d", e.name, e.pageNo, e.idx)
}

func extractImages(dirName string, filename string) ([]*extractedImage, error) {
	f, err := getFile(dirName, filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	reader, err := readPDF(f)
	if err != nil && err.Error() != "EOF not found" {
		return nil, err
	}

	var numPages int
	numPages, err = reader.GetNumPages()
	if err != nil {
		return nil, err
	}

	var (
		page               *model.PdfPage
		images, tempImages []*extractedImage
	)
	for pageNo := 1; pageNo <= numPages; pageNo++ {
		page, err = reader.GetPage(pageNo)
		if err != nil {
			return nil, err
		}

		tempImages, err = extractImagesOnPage(dirName, filename, page, pageNo)
		if err != nil {
			return nil, err
		}
		images = append(images, tempImages...)
	}
	return images, nil
}

func extractImagesInContentStream(filename, contents string, resources *model.PdfPageResources) ([]*extractedImage, error) {
	extractedImages := []*extractedImage{}
	processedXObjects := make(map[string]bool)
	cstreamParser := contentstream.NewContentStreamParser(contents)

	operations, err := cstreamParser.Parse()
	if err != nil {
		return nil, err
	}

	// Range through all the content stream operations.
	for _, op := range *operations {
		if !(op.Operand == "Do" && len(op.Params) == 1) {
			continue
		}
		// Do: XObject.
		name := op.Params[0].(*core.PdfObjectName)

		// Only process each one once.
		_, has := processedXObjects[string(*name)]
		if has {
			continue
		}
		processedXObjects[string(*name)] = true

		xobj, xtype := resources.GetXObjectByName(*name)
		if xtype == model.XObjectTypeImage {
			filterObj := core.TraceToDirectObject(xobj.Get("Filter"))
			if filterObj == nil {
				continue
			}

			method, ok := filterObj.(*core.PdfObjectName)
			if !ok {
				continue
			}

			if method.String() != core.StreamEncodingFilterNameJBIG2 {
				continue
			}

			ximg, err := resources.GetXObjectImageByName(*name)
			if err != nil {
				return nil, err
			}

			enc, ok := ximg.Filter.(*core.JBIG2Encoder)
			if !ok {
				return nil, fmt.Errorf("Filter encoder should be a JBIG2Encoder but is: %T", ximg.Filter)
			}

			extracted := &extractedImage{
				pdfImage:  ximg,
				jbig2Data: xobj.Stream,
				globals:   enc.Globals.ToDocumentGlobals(),
			}

			extractedImages = append(extractedImages, extracted)
		} else if xtype == model.XObjectTypeForm {
			// Go through the XObject Form content stream.
			xform, err := resources.GetXObjectFormByName(*name)
			if err != nil {
				return nil, err
			}

			formContent, err := xform.GetContentStream()
			if err != nil {
				return nil, err
			}

			// Process the content stream in the Form object too:
			formResources := xform.Resources
			if formResources == nil {
				formResources = resources
			}

			// Process the content stream in the Form object too:
			images, err := extractImagesInContentStream(filename, string(formContent), formResources)
			if err != nil {
				return nil, err
			}
			extractedImages = append(extractedImages, images...)
		}
	}
	return extractedImages, nil
}

func extractImagesOnPage(dirname, filename string, page *model.PdfPage, pageNo int) ([]*extractedImage, error) {
	contents, err := page.GetAllContentStreams()
	if err != nil {
		return nil, err
	}

	images, err := extractImagesInContentStream(filepath.Join(dirname, filename), contents, page.Resources)
	if err != nil {
		return nil, err
	}

	rawName := rawFileName(filename)
	for i, image := range images {
		image.name = rawName
		image.idx = i + 1
		image.pageNo = pageNo
	}
	return images, nil
}

func getFile(dirName, filename string) (*os.File, error) {
	return os.Open(filepath.Join(dirName, filename))
}

func rawFileName(filename string) string {
	if i := strings.LastIndex(filename, "."); i != -1 {
		return filename[:i]
	}
	return filename
}

func readFileNames(dirname, suffix string) ([]string, error) {
	var files []string
	err := filepath.Walk(dirname, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			if suffix != "" && !strings.HasSuffix(strings.ToLower(info.Name()), suffix) {
				return nil
			}
			files = append(files, info.Name())
		}
		return nil
	})
	return files, err
}

func readPDF(f *os.File, password ...string) (*model.PdfReader, error) {
	pdfReader, err := model.NewPdfReader(f)
	if err != nil {
		return nil, err
	}

	// check if is encrypted
	isEncrypted, err := pdfReader.IsEncrypted()
	if err != nil {
		return nil, err
	}

	if isEncrypted {
		auth, err := pdfReader.Decrypt([]byte(""))
		if err != nil {
			return nil, err
		}

		if !auth {
			if len(password) > 0 {
				auth, err = pdfReader.Decrypt([]byte(password[0]))
				if err != nil {
					return nil, err
				}
			}
			if !auth {
				return nil, fmt.Errorf("reading the file: '%s' failed. Invalid password provided", f.Name())
			}
		}
	}
	return pdfReader, nil
}

func writeExtractedImages(zw *zip.Writer, images ...*extractedImage) (err error) {
	h := md5.New()

	for _, img := range images {
		common.Log.Trace("Writing file: '%s'", img.fullName())
		f, err := zw.Create(img.fullName() + ".jpg")
		if err != nil {
			return err
		}

		cimg, err := img.pdfImage.ToImage()
		if err != nil {
			return err
		}

		gimg, err := cimg.ToGoImage()
		if err != nil {
			return err
		}

		multiWriter := io.MultiWriter(f, h)

		// write to file
		q := &jpeg.Options{Quality: 100}
		if err = jpeg.Encode(multiWriter, gimg, q); err != nil {
			return err
		}

		img.hash = hex.EncodeToString(h.Sum(nil))
		h.Reset()
	}
	return nil
}
