/*
 * This file is subject to the terms and conditions defined in
 * file 'LICENSE.md', which is part of this source code package.
 */

package annotator

import (
	"errors"
	"math"
	"strings"
	"unicode"

	"github.com/bcmmbaga/unipdf-agpl/v3/common"
	"github.com/bcmmbaga/unipdf-agpl/v3/contentstream"
	"github.com/bcmmbaga/unipdf-agpl/v3/contentstream/draw"
	"github.com/bcmmbaga/unipdf-agpl/v3/core"
	"github.com/bcmmbaga/unipdf-agpl/v3/internal/textencoding"
	"github.com/bcmmbaga/unipdf-agpl/v3/model"
)

// FieldAppearance implements interface model.FieldAppearanceGenerator and generates appearance streams
// for fields taking into account what value is in the field. A common use case is for generating the
// appearance stream prior to flattening fields.
//
// If `OnlyIfMissing` is true, the field appearance is generated only for fields that do not have an
// appearance stream specified.
// If `RegenerateTextFields` is true, all text fields are regenerated (even if OnlyIfMissing is true).
type FieldAppearance struct {
	OnlyIfMissing        bool
	RegenerateTextFields bool
	style                *AppearanceStyle
}

// AppearanceStyle defines style parameters for appearance stream generation.
type AppearanceStyle struct {
	// How much of Rect height to fill when autosizing text.
	AutoFontSizeFraction float64

	// CheckmarkRune is a rune used for check mark in checkboxes (for ZapfDingbats font).
	CheckmarkRune rune

	BorderSize  float64
	BorderColor model.PdfColor
	FillColor   model.PdfColor

	// Multiplier for lineheight for multi line text.
	MultilineLineHeight   float64
	MultilineVAlignMiddle bool // Defaults to top.

	// Visual guide checking alignment of field contents (debugging).
	DrawAlignmentReticle bool

	// Allow field MK appearance characteristics to override style settings.
	AllowMK bool

	// Fonts holds appearance styles for fonts.
	Fonts *AppearanceFontStyle
}

// AppearanceFontStyle defines font style characteristics for form fields,
// used in the filling/flattening process.
type AppearanceFontStyle struct {
	// Fallback represents a global font fallback, used for fields which do
	// not specify a font in their default appearance (DA). The fallback is
	// also used if there is a font specified in the DA, but it is not
	// found in the AcroForm resources (DR).
	Fallback *AppearanceFont

	// FieldFallbacks defines font fallbacks for specific fields. The map keys
	// represent the names of the fields (which can be specified by their
	// partial or full names). Specific field fallback fonts take precedence
	// over the global font fallback.
	FieldFallbacks map[string]*AppearanceFont

	// ForceReplace forces the replacement of fonts in the filling/flattening
	// process, even if the default appearance (DA) specify a valid font.
	// If no fallback font is provided, setting this field has no effect.
	ForceReplace bool
}

// AppearanceFont represents a font used for generating the appearance of a
// field in the filling/flattening process.
type AppearanceFont struct {
	// Name represents the name of the font which will be added to the
	// AcroForm resources (DR).
	Name string

	// Font represents the actual font used for the field appearance.
	Font *model.PdfFont

	// Size represents the size of the font used for the field appearance.
	// If size is 0, a default font size will be used.
	// The default font size is calculated using the available annotation
	// height and the AutoFontSizeFraction of the AppearanceStyle.
	Size float64
}

type quadding int

const (
	quaddingLeft   quadding = 0
	quaddingCenter quadding = 1
	quaddingRight  quadding = 2
)

// SetStyle applies appearance `style` to `fa`.
func (fa *FieldAppearance) SetStyle(style AppearanceStyle) {
	fa.style = &style
}

// Style returns the appearance style of `fa`. If not specified, returns default style.
func (fa FieldAppearance) Style() AppearanceStyle {
	if fa.style != nil {
		return *fa.style
	}
	// Default values returned if style not set.
	return AppearanceStyle{
		AutoFontSizeFraction:  0.65,
		CheckmarkRune:         '✔',
		BorderSize:            0.0,
		BorderColor:           model.NewPdfColorDeviceGray(0),
		FillColor:             model.NewPdfColorDeviceGray(1),
		MultilineLineHeight:   1.2,
		MultilineVAlignMiddle: false,
		DrawAlignmentReticle:  false,
		AllowMK:               true,
	}
}

// GenerateAppearanceDict generates an appearance dictionary for widget annotation `wa` for the `field` in `form`.
// Implements interface model.FieldAppearanceGenerator.
func (fa FieldAppearance) GenerateAppearanceDict(form *model.PdfAcroForm, field *model.PdfField, wa *model.PdfAnnotationWidget) (*core.PdfObjectDictionary, error) {
	common.Log.Trace("GenerateAppearanceDict for %v  V: %+v", field.PartialName(), field.V)
	_, isText := field.GetContext().(*model.PdfFieldText)

	appDict, has := core.GetDict(wa.AP)
	if has && fa.OnlyIfMissing && (!isText || !fa.RegenerateTextFields) {
		common.Log.Trace("Already populated - ignoring")
		return appDict, nil
	}
	if form.DR == nil {
		form.DR = model.NewPdfPageResources()
	}

	// Generate the appearance.
	switch t := field.GetContext().(type) {
	case *model.PdfFieldText:
		ftxt := t

		// Handle special cases.
		switch {
		case ftxt.Flags().Has(model.FieldFlagPassword):
			// Should never store password values.
			return nil, nil
		case ftxt.Flags().Has(model.FieldFlagFileSelect):
			// Not supported.
			return nil, nil
		case ftxt.Flags().Has(model.FieldFlagComb):
			// Special handling for comb. Only if max len is set.
			if ftxt.MaxLen != nil {
				appDict, err := genFieldTextCombAppearance(wa, ftxt, form.DR, fa.Style())
				if err != nil {
					return nil, err
				}
				return appDict, nil
			}
		}

		appDict, err := genFieldTextAppearance(wa, ftxt, form.DR, fa.Style())
		if err != nil {
			return nil, err
		}

		return appDict, nil
	case *model.PdfFieldButton:
		fbtn := t
		if fbtn.IsCheckbox() {
			appDict, err := genFieldCheckboxAppearance(wa, fbtn, form.DR, fa.Style())
			if err != nil {
				return nil, err
			}
			return appDict, nil
		}

		common.Log.Debug("TODO: UNHANDLED button type: %+v", fbtn.GetType())
	case *model.PdfFieldChoice:
		fch := t
		switch {
		case fch.Flags().Has(model.FieldFlagCombo):
			appDict, err := genFieldComboboxAppearance(form, wa, fch, fa.Style())
			if err != nil {
				return nil, err
			}
			return appDict, nil
		default:
			common.Log.Debug("TODO: UNHANDLED choice field with flags: %s", fch.Flags().String())
		}

	default:
		common.Log.Debug("TODO: UNHANDLED field type: %T", t)
	}

	return nil, nil
}

// genTextAppearance generates the appearance stream for widget annotation `wa` with text field `ftxt`.
// It requires access to the form resources DR entry via `dr`.
func genFieldTextAppearance(wa *model.PdfAnnotationWidget, ftxt *model.PdfFieldText, dr *model.PdfPageResources, style AppearanceStyle) (*core.PdfObjectDictionary, error) {
	resources := model.NewPdfPageResources()

	// Get bounding Rect.
	array, ok := core.GetArray(wa.Rect)
	if !ok {
		return nil, errors.New("invalid Rect")
	}
	rect, err := model.NewPdfRectangle(*array)
	if err != nil {
		return nil, err
	}
	width, height := rect.Width(), rect.Height()
	bboxWidth, bboxHeight := width, height

	mkDict, has := core.GetDict(wa.MK)
	if has {
		bsDict, _ := core.GetDict(wa.BS)
		err := style.applyAppearanceCharacteristics(mkDict, bsDict, nil)
		if err != nil {
			return nil, err
		}
	}

	// Get and process the default appearance string (DA) operands.
	daOps, err := contentstream.NewContentStreamParser(getDA(ftxt.PdfField)).Parse()
	if err != nil {
		return nil, err
	}

	cc := contentstream.NewContentCreator()

	if style.BorderSize > 0 {
		drawRect(cc, style, width, height)
	}

	if style.DrawAlignmentReticle {
		// Alignment reticle.
		style2 := style
		style2.BorderSize = 0.2
		drawAlignmentReticle(cc, style2, width, height)
	}

	cc.Add_BMC("Tx")
	cc.Add_q()

	// Apply rotation if present.
	// Update width and height, as the appearance is generated based on
	// the bounding of the annotation with no rotation.
	width, height = style.applyRotation(mkDict, width, height, cc)

	// Graphic state changes.
	cc.Add_BT()

	// Process DA operands.
	apFont, hasTf, err := style.processDA(ftxt.PdfField, daOps, dr, resources, cc)
	if err != nil {
		return nil, err
	}

	font := apFont.Font
	fontsize := apFont.Size
	fontname := core.MakeName(apFont.Name)
	autosize := fontsize == 0
	if autosize && hasTf {
		fontsize = height * style.AutoFontSizeFraction
	}

	encoder := font.Encoder()
	if encoder == nil {
		common.Log.Debug("WARN: font encoder is nil. Assuming identity encoder. Output may be incorrect.")
		encoder = textencoding.NewIdentityTextEncoder("Identity-H")
	}

	fdescriptor, err := font.GetFontDescriptor()
	if err != nil {
		common.Log.Debug("Error: Unable to get font descriptor")
	}

	var text string
	if str, ok := core.GetString(ftxt.V); ok {
		text = str.Decoded()
	}

	// If no text, no appearance needed.
	if len(text) == 0 {
		return nil, nil
	}

	lines := []string{text}

	// Handle multi line fields.
	isMultiline := false
	if ftxt.Flags().Has(model.FieldFlagMultiline) {
		isMultiline = true
		text = strings.Replace(text, "\r\n", "\n", -1)
		text = strings.Replace(text, "\r", "\n", -1)
		lines = strings.Split(text, "\n")
	}

	maxLinewidth := 0.0
	textlines := 0
	if encoder != nil {
		l := len(lines)
		i := 0
		for i < l {
			var lastwidth float64
			lastbreakindex := -1
			linewidth := 0.0
			for index, r := range lines[i] {
				if r == ' ' {
					lastbreakindex = index
					lastwidth = linewidth
				}
				metrics, has := font.GetRuneMetrics(r)
				if !has {
					common.Log.Debug("Font does not have rune metrics for %v - skipping", r)
					continue
				}
				linewidth += metrics.Wx

				if isMultiline && !autosize && fontsize*linewidth/1000.0 > width && lastbreakindex > 0 {
					part2 := lines[i][lastbreakindex+1:]

					if i < len(lines)-1 {
						lines[i+1] += part2
					} else {
						lines = append(lines, part2)
						l++
					}
					lines[i] = lines[i][0:lastbreakindex]
					linewidth = lastwidth
					break
				}
			}
			if linewidth > maxLinewidth {
				maxLinewidth = linewidth
			}

			lines[i] = string(encoder.Encode(lines[i]))
			if len(lines[i]) > 0 {
				textlines++
			}
			i++
		}
	}

	tx := 2.0

	// Check if text goes out of bounds, if goes out of bounds, then adjust font size until just within bounds.
	if fontsize == 0 || autosize && maxLinewidth > 0 && tx+maxLinewidth*fontsize/1000.0 > width {
		// TODO(gunnsth): Add to style options.
		fontsize = 0.95 * 1000.0 * (width - tx) / maxLinewidth
	}

	alignment := quaddingLeft
	// Account for horizontal alignment (quadding).
	{
		if val, has := core.GetIntVal(ftxt.Q); has {
			switch val {
			case 0: // Left aligned.
				alignment = quaddingLeft
			case 1: // Centered.
				alignment = quaddingCenter
			case 2: // Right justified.
				alignment = quaddingRight
			default:
				common.Log.Debug("ERROR: Unsupported quadding: %d - using left alignment", val)
			}
		}
	}

	lh := style.MultilineLineHeight

	lineheight := fontsize
	if isMultiline && textlines > 1 {
		lineheight = lh * fontsize
	}

	var fcapheight float64
	if fdescriptor != nil {
		fcapheight, err = fdescriptor.GetCapHeight()
		if err != nil {
			common.Log.Debug("ERROR: Unable to get font CapHeight: %v", err)
		}
	}
	if int(fcapheight) <= 0 {
		common.Log.Debug("WARN: CapHeight not available - setting to 1000")
		fcapheight = 1000
	}
	capheight := fcapheight / 1000.0 * fontsize

	// Vertical alignment.
	ty := 0.0
	{
		textheight := float64(textlines) * lineheight
		if autosize && ty+textheight > height {
			fontsize = 0.95 * (height - ty) / float64(textlines)
			lineheight = fontsize
			if isMultiline && textlines > 1 {
				lineheight = lh * fontsize
			}
			capheight = fcapheight / 1000.0 * fontsize
			textheight = float64(textlines) * lineheight
		}

		if height > textheight {
			if isMultiline {
				if style.MultilineVAlignMiddle {
					a := (height - textheight) / 2.0
					b := a + textheight - lineheight
					ty = b
				} else {
					// Top.
					ty = height - lineheight
					ty -= fontsize * 0.5
				}
			} else {
				ty = (height - capheight) / 2.0
			}
		}
	}

	cc.Add_Tf(*fontname, fontsize)
	cc.Add_Td(tx, ty)
	tx0 := tx
	x := tx
	for i, line := range lines {
		wLine := 0.0
		for _, r := range line {
			metrics, has := font.GetRuneMetrics(r)
			if !has {
				continue
			}
			wLine += metrics.Wx
		}
		linewidth := wLine / 1000.0 * fontsize
		remaining := width - linewidth

		var xnew float64
		switch alignment {
		case quaddingLeft:
			xnew = tx0
		case quaddingCenter:
			xnew = remaining / 2
		case quaddingRight:
			xnew = remaining
		}
		tx = xnew - x
		if tx > 0.0 {
			cc.Add_Td(tx, 0)
		}
		x = xnew

		cc.Add_Tj(*core.MakeString(line))

		if i < len(lines)-1 {
			cc.Add_Td(0, -lineheight*lh)
		}
	}

	cc.Add_ET()
	cc.Add_Q()
	cc.Add_EMC()

	xform := model.NewXObjectForm()
	xform.Resources = resources
	xform.BBox = core.MakeArrayFromFloats([]float64{0, 0, bboxWidth, bboxHeight})
	xform.SetContentStream(cc.Bytes(), defStreamEncoder())

	apDict := core.MakeDict()
	apDict.Set("N", xform.ToPdfObject())

	return apDict, nil
}

// genFieldTextCombAppearance generates an appearance dictionary for a comb text field where the width is split
// into equal size boxes.
func genFieldTextCombAppearance(wa *model.PdfAnnotationWidget, ftxt *model.PdfFieldText, dr *model.PdfPageResources, style AppearanceStyle) (*core.PdfObjectDictionary, error) {
	resources := model.NewPdfPageResources()

	// Get bounding Rect.
	array, ok := core.GetArray(wa.Rect)
	if !ok {
		return nil, errors.New("invalid Rect")
	}
	rect, err := model.NewPdfRectangle(*array)
	if err != nil {
		return nil, err
	}
	width, height := rect.Width(), rect.Height()
	bboxWidth, bboxHeight := width, height

	mkDict, has := core.GetDict(wa.MK)
	if has {
		bsDict, _ := core.GetDict(wa.BS)
		err := style.applyAppearanceCharacteristics(mkDict, bsDict, nil)
		if err != nil {
			return nil, err
		}
	}

	maxLen, has := core.GetIntVal(ftxt.MaxLen)
	if !has {
		return nil, errors.New("maxlen not set")
	}
	if maxLen <= 0 {
		return nil, errors.New("maxLen invalid")
	}

	boxwidth := float64(width) / float64(maxLen)

	// Get and process the default appearance string (DA) operands.
	daOps, err := contentstream.NewContentStreamParser(getDA(ftxt.PdfField)).Parse()
	if err != nil {
		return nil, err
	}

	cc := contentstream.NewContentCreator()
	if style.BorderSize > 0 {
		drawRect(cc, style, width, height)
	}
	if style.DrawAlignmentReticle {
		// Alignment reticle.
		style2 := style
		style2.BorderSize = 0.2
		drawAlignmentReticle(cc, style2, width, height)
	}
	cc.Add_BMC("Tx")
	cc.Add_q()

	// Apply rotation if present.
	// Update width and height, as the appearance is generated based on
	// the bounding of the annotation with no rotation.
	width, height = style.applyRotation(mkDict, width, height, cc)

	// Graphic state changes.
	cc.Add_BT()

	// Process DA operands.
	apFont, hasTf, err := style.processDA(ftxt.PdfField, daOps, dr, resources, cc)
	if err != nil {
		return nil, err
	}

	font := apFont.Font
	fontname := core.MakeName(apFont.Name)
	fontsize := apFont.Size
	autosize := fontsize == 0
	if autosize && hasTf {
		fontsize = height * style.AutoFontSizeFraction
	}

	encoder := font.Encoder()
	if encoder == nil {
		common.Log.Debug("WARN: font encoder is nil. Assuming identity encoder. Output may be incorrect.")
		encoder = textencoding.NewIdentityTextEncoder("Identity-H")
	}

	var text string
	if str, ok := core.GetString(ftxt.V); ok {
		text = str.Decoded()
	}

	cc.Add_Tf(*fontname, fontsize)

	// Get max glyph height.
	var maxGlyphWy float64
	for _, r := range text {
		metrics, found := font.GetRuneMetrics(r)
		if !found {
			common.Log.Debug("ERROR: Rune not found in font: %v - skipping over", r)
			continue
		}
		wy := metrics.Wy
		if int(wy) <= 0 {
			wy = metrics.Wx
		}
		if wy > maxGlyphWy {
			maxGlyphWy = wy
		}
	}
	if int(maxGlyphWy) == 0 {
		common.Log.Debug("ERROR: Unable to determine max glyph size - using 1000")
		maxGlyphWy = 1000
	}

	fdescriptor, err := font.GetFontDescriptor()
	if err != nil {
		common.Log.Debug("Error: Unable to get font descriptor")
	}
	var fcapheight float64
	if fdescriptor != nil {
		fcapheight, err = fdescriptor.GetCapHeight()
		if err != nil {
			common.Log.Debug("ERROR: Unable to get font CapHeight: %v", err)
		}
	}
	if int(fcapheight) <= 0 {
		common.Log.Debug("WARN: CapHeight not available - setting to 1000")
		fcapheight = 1000.0
	}
	capheight := fcapheight / 1000.0 * fontsize

	// Vertical alignment.
	ty := 0.0
	lineheight := 1.0 * fontsize * (maxGlyphWy / 1000.0)
	{
		textheight := lineheight
		// If autosize and going out of bounds, reduce to fit.
		if autosize && ty+textheight > height {
			fontsize = 0.95 * (height - ty)
			lineheight = 1.0 * fontsize
			textheight = lineheight
			capheight = fcapheight / 1000.0 * fontsize
		}

		if height > capheight {
			ty = (height - capheight) / 2.0
		}
	}
	cc.Add_Td(0, ty)

	if quadding, has := core.GetIntVal(ftxt.Q); has {
		switch quadding {
		case 2: // Right justified.
			if len(text) < maxLen {
				offset := float64(maxLen-len(text)) * boxwidth
				cc.Add_Td(offset, 0)
			}
		}
	}

	for i, r := range text {
		tx := 2.0
		encoded := string(r)
		if encoder != nil {
			metrics, found := font.GetRuneMetrics(r)
			if !found {
				common.Log.Debug("ERROR: Rune not found in font: %v - skipping over", r)
				continue
			}

			encoded = string(encoder.Encode(encoded))

			// Calculate indent such that the glyph is positioned in the center.
			glyphwidth := fontsize * metrics.Wx / 1000.0
			calcIndent := (boxwidth - glyphwidth) / 2
			tx = calcIndent
		}

		cc.Add_Td(tx, 0)
		cc.Add_Tj(*core.MakeString(encoded))

		if i != len(text)-1 {
			cc.Add_Td(boxwidth-tx, 0)
		}
	}

	cc.Add_ET()
	cc.Add_Q()
	cc.Add_EMC()

	xform := model.NewXObjectForm()
	xform.Resources = resources
	xform.BBox = core.MakeArrayFromFloats([]float64{0, 0, bboxWidth, bboxHeight})
	xform.SetContentStream(cc.Bytes(), defStreamEncoder())

	apDict := core.MakeDict()
	apDict.Set("N", xform.ToPdfObject())

	return apDict, nil
}

// genFieldCheckboxAppearance generates an appearance dictionary for a widget annotation `wa` referenced by
// a button field `fbtn` with form resources `dr` (DR).
func genFieldCheckboxAppearance(wa *model.PdfAnnotationWidget, fbtn *model.PdfFieldButton, dr *model.PdfPageResources, style AppearanceStyle) (*core.PdfObjectDictionary, error) {
	// Get bounding Rect.
	array, ok := core.GetArray(wa.Rect)
	if !ok {
		return nil, errors.New("invalid Rect")
	}
	rect, err := model.NewPdfRectangle(*array)
	if err != nil {
		return nil, err
	}
	width, height := rect.Width(), rect.Height()
	bboxWidth, bboxHeight := width, height

	common.Log.Debug("Checkbox, wa BS: %v", wa.BS)

	zapfdb, err := model.NewStandard14Font("ZapfDingbats")
	if err != nil {
		return nil, err
	}

	mkDict, has := core.GetDict(wa.MK)
	if has {
		bsDict, _ := core.GetDict(wa.BS)
		err := style.applyAppearanceCharacteristics(mkDict, bsDict, zapfdb)
		if err != nil {
			return nil, err
		}
	}

	xformOn := model.NewXObjectForm()
	{
		cc := contentstream.NewContentCreator()
		if style.BorderSize > 0 {
			drawRect(cc, style, width, height)
		}

		if style.DrawAlignmentReticle {
			// Alignment reticle.
			style2 := style
			style2.BorderSize = 0.2
			drawAlignmentReticle(cc, style2, width, height)
		}

		// Apply rotation if present.
		// Update width and height, as the appearance is generated based on
		// the bounding of the annotation with no rotation.
		width, height = style.applyRotation(mkDict, width, height, cc)

		fontsize := style.AutoFontSizeFraction * height

		checkmetrics, ok := zapfdb.GetRuneMetrics(style.CheckmarkRune)
		if !ok {
			return nil, errors.New("glyph not found")
		}
		enc := zapfdb.Encoder()
		checkstr := enc.Encode(string(style.CheckmarkRune))

		checkwidth := checkmetrics.Wx * fontsize / 1000.0
		// TODO: Get bbox of specific glyph that is chosen.  Choice of specific value will cause slight
		// deviations for other glyphs, but should be fairly close.
		fcheckheight := 705.0 // From AFM for code 52.
		checkheight := fcheckheight / 1000.0 * fontsize

		tx := 2.0
		ty := 1.0
		if checkwidth < width {
			tx = (width - checkwidth) / 2.0
		}
		if checkheight < height {
			ty = (height - checkheight) / 2.0
		}

		cc.Add_q().
			Add_g(0).
			Add_BT().
			Add_Tf("ZaDb", fontsize).
			Add_Td(tx, ty).
			Add_Tj(*core.MakeStringFromBytes(checkstr)).
			Add_ET().
			Add_Q()

		xformOn.Resources = model.NewPdfPageResources()
		xformOn.Resources.SetFontByName("ZaDb", zapfdb.ToPdfObject())
		xformOn.BBox = core.MakeArrayFromFloats([]float64{0, 0, bboxWidth, bboxHeight})
		xformOn.SetContentStream(cc.Bytes(), defStreamEncoder())
	}

	xformOff := model.NewXObjectForm()
	{
		cc := contentstream.NewContentCreator()
		if style.BorderSize > 0 {
			drawRect(cc, style, width, height)
		}
		xformOff.BBox = core.MakeArrayFromFloats([]float64{0, 0, bboxWidth, bboxHeight})
		xformOff.SetContentStream(cc.Bytes(), defStreamEncoder())
	}

	dchoiceapp := core.MakeDict()
	dchoiceapp.Set("Off", xformOff.ToPdfObject())
	dchoiceapp.Set("Yes", xformOn.ToPdfObject())

	appDict := core.MakeDict()
	appDict.Set("N", dchoiceapp)

	return appDict, nil
}

// genFieldComboboxAppearance generates an appearance dictionary for a widget annotation `wa` referenced by a
// combobox choice field `fch` with form resources (DR) `dr`.
func genFieldComboboxAppearance(form *model.PdfAcroForm, wa *model.PdfAnnotationWidget, fch *model.PdfFieldChoice, style AppearanceStyle) (*core.PdfObjectDictionary, error) {
	// Get bounding Rect.
	array, ok := core.GetArray(wa.Rect)
	if !ok {
		return nil, errors.New("invalid Rect")
	}
	rect, err := model.NewPdfRectangle(*array)
	if err != nil {
		return nil, err
	}
	width, height := rect.Width(), rect.Height()

	common.Log.Debug("Choice, wa BS: %v", wa.BS)

	// Get and process the default appearance string (DA) operands.
	daOps, err := contentstream.NewContentStreamParser(getDA(fch.PdfField)).Parse()
	if err != nil {
		return nil, err
	}

	mkDict, has := core.GetDict(wa.MK)
	if has {
		bsDict, _ := core.GetDict(wa.BS)
		err := style.applyAppearanceCharacteristics(mkDict, bsDict, nil)
		if err != nil {
			return nil, err
		}
	}

	// See section 12.7.4.4 "Choice Fields" (pp. 444-446 PDF32000_2008).
	dchoiceapp := core.MakeDict()
	for _, optObj := range fch.Opt.Elements() {
		if optArr, ok := core.GetArray(optObj); ok && optArr.Len() == 2 {
			optObj = optArr.Get(1)
		}

		var optstr string
		if opt, ok := core.GetString(optObj); ok {
			optstr = opt.Decoded()
		} else if opt, ok := core.GetName(optObj); ok {
			optstr = opt.String()
		} else {
			common.Log.Debug("ERROR: Opt not a name/string - %T", optObj)
			return nil, errors.New("not a name/string")
		}

		if len(optstr) > 0 {
			xform, err := makeComboboxTextXObjForm(fch.PdfField, width, height, optstr, style, daOps, form.DR, mkDict)
			if err != nil {
				return nil, err
			}

			dchoiceapp.Set(*core.MakeName(optstr), xform.ToPdfObject())
		}
	}

	appDict := core.MakeDict()
	appDict.Set("N", dchoiceapp)

	return appDict, nil
}

// Make a text-based XObj Form.
func makeComboboxTextXObjForm(field *model.PdfField, width, height float64,
	text string, style AppearanceStyle, daOps *contentstream.ContentStreamOperations,
	dr *model.PdfPageResources, mkDict *core.PdfObjectDictionary) (*model.XObjectForm, error) {
	resources := model.NewPdfPageResources()
	bboxWidth, bboxHeight := width, height

	cc := contentstream.NewContentCreator()
	if style.BorderSize > 0 {
		drawRect(cc, style, width, height)
	}
	if style.DrawAlignmentReticle {
		// Alignment reticle.
		style2 := style
		style2.BorderSize = 0.2
		drawAlignmentReticle(cc, style2, width, height)
	}
	cc.Add_BMC("Tx")
	cc.Add_q()
	// Graphic state changes.
	cc.Add_BT()

	// Apply rotation if present.
	// Update width and height, as the appearance is generated based on
	// the bounding of the annotation with no rotation.
	width, height = style.applyRotation(mkDict, width, height, cc)

	// Process DA operands.
	apFont, hasTf, err := style.processDA(field, daOps, dr, resources, cc)
	if err != nil {
		return nil, err
	}

	font := apFont.Font
	fontsize := apFont.Size
	fontname := core.MakeName(apFont.Name)
	autosize := fontsize == 0
	if autosize && hasTf {
		fontsize = height * style.AutoFontSizeFraction
	}

	encoder := font.Encoder()
	if encoder == nil {
		common.Log.Debug("WARN: font encoder is nil. Assuming identity encoder. Output may be incorrect.")
		encoder = textencoding.NewIdentityTextEncoder("Identity-H")
	}

	// If no text, no appearance needed.
	if len(text) == 0 {
		return nil, nil
	}

	tx := 2.0 // Default left margin. // TODO(gunnsth): Add to style options.

	linewidth := 0.0
	if encoder != nil {
		for _, r := range text {
			metrics, has := font.GetRuneMetrics(r)
			if !has {
				common.Log.Debug("Font does not have rune metrics for %v - skipping", r)
				continue
			}
			linewidth += metrics.Wx
		}

		text = string(encoder.Encode(text))
	}

	// Check if text goes out of bounds, if goes out of bounds, then adjust font size until just within bounds.
	if fontsize == 0 || autosize && linewidth > 0 && tx+linewidth*fontsize/1000.0 > width {
		// TODO(gunnsth): Add to style options.
		fontsize = 0.95 * 1000.0 * (width - tx) / linewidth
	}

	lineheight := 1.0 * fontsize

	// Vertical alignment.
	ty := 2.0
	{
		textheight := lineheight
		if autosize && ty+textheight > height {
			fontsize = 0.95 * (height - ty)
			lineheight = 1.0 * fontsize
			textheight = lineheight
		}

		if height > textheight {
			ty = (height - textheight) / 2.0
			ty += 1.50 // TODO(gunnsth): Make configurable/part of style parameter.
		}
	}

	cc.Add_Tf(*fontname, fontsize)
	cc.Add_Td(tx, ty)
	cc.Add_Tj(*core.MakeString(text))

	cc.Add_ET()
	cc.Add_Q()
	cc.Add_EMC()

	xform := model.NewXObjectForm()
	xform.Resources = resources
	xform.BBox = core.MakeArrayFromFloats([]float64{0, 0, bboxWidth, bboxHeight})
	xform.SetContentStream(cc.Bytes(), defStreamEncoder())

	return xform, nil
}

// getDA returns the default appearance text (DA) for a given field `ftxt`.
// If not set for `ftxt` then checks if set by Parent (inherited), otherwise
// returns "".
func getDA(field *model.PdfField) string {
	if field == nil {
		return ""
	}

	ftxt, ok := field.GetContext().(*model.PdfFieldText)
	if !ok {
		return getDA(field.Parent)
	}

	if ftxt.DA != nil {
		return ftxt.DA.Str()
	}

	return getDA(ftxt.Parent)
}

// drawRect draws the annotation Rectangle.
// TODO(gunnsth): Apply clipping so annotation contents cannot go outside Rect.
func drawRect(cc *contentstream.ContentCreator, style AppearanceStyle, width, height float64) {
	cc.Add_q().
		Add_re(0, 0, width, height).
		Add_w(style.BorderSize).
		SetStrokingColor(style.BorderColor).
		SetNonStrokingColor(style.FillColor).
		Add_B().
		Add_Q()
}

// drawAlignmentReticle draws the Rect box with a reticle on top for alignment guidance.
func drawAlignmentReticle(cc *contentstream.ContentCreator, style AppearanceStyle, width, height float64) {
	cc.Add_q().
		Add_re(0, 0, width, height).
		Add_re(0, height/2, width, height/2).
		Add_re(0, 0, width, height).
		Add_re(width/2, 0, width/2, height).
		Add_w(style.BorderSize).
		SetStrokingColor(style.BorderColor).
		SetNonStrokingColor(style.FillColor).
		Add_B().
		Add_Q()
}

// Apply appearance characteristics from an MK dictionary `mkDict` to appearance `style`.
// `font` is necessary when the "normal caption" (CA) field is specified (checkboxes).
func (style *AppearanceStyle) applyAppearanceCharacteristics(mkDict *core.PdfObjectDictionary, bsDict *core.PdfObjectDictionary, font *model.PdfFont) error {
	if !style.AllowMK {
		return nil
	}

	if bsDict != nil {
		if w, err := core.GetNumberAsFloat(bsDict.Get("W")); err == nil {
			style.BorderSize = w
		}
	}

	// Normal caption.
	if CA, has := core.GetString(mkDict.Get("CA")); has && font != nil {
		encoded := CA.Bytes()
		if len(encoded) != 0 {
			runes := []rune(font.Encoder().Decode(encoded))
			if len(runes) == 1 {
				style.CheckmarkRune = runes[0]
			}
		}
	}

	// Border color.
	if BC, has := core.GetArray(mkDict.Get("BC")); has {
		bc, err := BC.ToFloat64Array()
		if err != nil {
			return err
		}

		switch len(bc) {
		case 1:
			style.BorderColor = model.NewPdfColorDeviceGray(bc[0])
		case 3:
			style.BorderColor = model.NewPdfColorDeviceRGB(bc[0], bc[1], bc[2])
		case 4:
			style.BorderColor = model.NewPdfColorDeviceCMYK(bc[0], bc[1], bc[2], bc[3])
		default:
			common.Log.Debug("ERROR: BC - Invalid number of color components (%d)", len(bc))
		}
	}

	// Border background (fill).
	if BG, has := core.GetArray(mkDict.Get("BG")); has {
		bg, err := BG.ToFloat64Array()
		if err != nil {
			return err
		}

		switch len(bg) {
		case 1:
			style.FillColor = model.NewPdfColorDeviceGray(bg[0])
		case 3:
			style.FillColor = model.NewPdfColorDeviceRGB(bg[0], bg[1], bg[2])
		case 4:
			style.FillColor = model.NewPdfColorDeviceCMYK(bg[0], bg[1], bg[2], bg[3])
		default:
			common.Log.Debug("ERROR: BG - Invalid number of color components (%d)", len(bg))
		}
	}

	return nil
}

// applyRotation applies the rotation specified by the MK dictionary,
// if present. The method returns the width and height of the annotation
// rectangle with no rotation.
func (style *AppearanceStyle) applyRotation(mkDict *core.PdfObjectDictionary,
	width, height float64, cc *contentstream.ContentCreator) (float64, float64) {
	if !style.AllowMK {
		return width, height
	}
	if mkDict == nil {
		return width, height
	}

	// Extract rotation from the MK dictionary.
	rotation, _ := core.GetNumberAsFloat(mkDict.Get("R"))
	if rotation == 0 {
		return width, height
	}

	// Calculate bounding box before rotation.
	revRotation := -rotation
	bbox := draw.Path{Points: []draw.Point{
		draw.NewPoint(0, 0).Rotate(revRotation),
		draw.NewPoint(width, 0).Rotate(revRotation),
		draw.NewPoint(0, height).Rotate(revRotation),
		draw.NewPoint(width, height).Rotate(revRotation),
	}}.GetBoundingBox()

	// Apply rotation.
	cc.RotateDeg(rotation)
	cc.Translate(bbox.X, bbox.Y)

	return bbox.Width, bbox.Height
}

// processDA adds the operands found in the field default appearance stream to
// the provided content stream creator. It also provides a fallback font, based
// on the configuration of the AppearanceStyle, if no valid font is specified
// in the default appearance. The method returns the font to be used when
// generating the appearance of the field and a boolean value specifying if
// the DA stream contains any Tf operands.
func (style *AppearanceStyle) processDA(field *model.PdfField,
	daOps *contentstream.ContentStreamOperations, dr, resources *model.PdfPageResources,
	cc *contentstream.ContentCreator) (*AppearanceFont, bool, error) {
	// Check for fallback fonts.
	var fallbackFont *AppearanceFont
	var forceReplace bool
	if style.Fonts != nil {
		// Use global fallback, if one is specified.
		if style.Fonts.Fallback != nil {
			fallbackFont = style.Fonts.Fallback
		}

		// Use field fallback, if one is specified.
		if fieldFallbacks := style.Fonts.FieldFallbacks; fieldFallbacks != nil {
			if fbFont, ok := fieldFallbacks[field.PartialName()]; ok {
				fallbackFont = fbFont
			} else if fullName, err := field.FullName(); err == nil {
				if fbFont, ok := fieldFallbacks[fullName]; ok {
					fallbackFont = fbFont
				}
			}
		}

		forceReplace = style.Fonts.ForceReplace
	}

	// Iterate over the DA operands and extract the font, if specified.
	var fontName string
	var fontSize float64
	var hasTf bool
	if daOps != nil {
		for _, op := range *daOps {
			if op.Operand == "Tf" && len(op.Params) == 2 {
				if name, ok := core.GetNameVal(op.Params[0]); ok {
					fontName = name
				}
				if size, err := core.GetNumberAsFloat(op.Params[1]); err == nil {
					fontSize = size
				}
				hasTf = true
				continue
			}
			cc.AddOperand(*op)
		}
	}

	var apFont *AppearanceFont
	var apFontObj core.PdfObject
	if forceReplace && fallbackFont != nil {
		apFont = fallbackFont
	} else {
		// Check if font name was found in the DA stream and search it in the resources.
		if dr != nil && fontName != "" {
			if obj, ok := dr.GetFontByName(*core.MakeName(fontName)); ok {
				if font, err := model.NewPdfFontFromPdfObject(obj); err == nil {
					apFontObj = obj
					apFont = &AppearanceFont{Name: fontName, Font: font, Size: fontSize}
				} else {
					common.Log.Debug("ERROR: could not load appearance font: %v", err)
				}
			}
		}

		// Use fallback font, if one was specified.
		if apFont == nil && fallbackFont != nil {
			apFont = fallbackFont
		}

		// Use default fallback font (Helvetica).
		if apFont == nil {
			font, err := model.NewStandard14Font("Helvetica")
			if err != nil {
				return nil, false, err
			}
			apFont = &AppearanceFont{Name: "Helv", Font: font, Size: fontSize}
		}
	}

	// Add appearance font to the form resources (DR).
	apFontName := *core.MakeName(apFont.Name)
	if apFontObj == nil {
		apFontObj = apFont.Font.ToPdfObject()
	}
	if dr != nil && !dr.HasFontByName(apFontName) {
		dr.SetFontByName(apFontName, apFontObj)
	}
	if resources != nil && !resources.HasFontByName(apFontName) {
		resources.SetFontByName(apFontName, apFontObj)
	}

	return apFont, hasTf, nil
}

// WrapContentStream ensures that the entire content stream for a `page` is wrapped within q ... Q operands.
// Ensures that following operands that are added are not affected by additional operands that are added.
// Implements interface model.ContentStreamWrapper.
func (fa FieldAppearance) WrapContentStream(page *model.PdfPage) error {
	cstream, err := page.GetAllContentStreams()
	if err != nil {
		return err
	}
	csp := contentstream.NewContentStreamParser(cstream)
	operands, err := csp.Parse()
	if err != nil {
		return err
	}
	operands.WrapIfNeeded()

	cstreams := []string{operands.String()}
	return page.SetContentStreams(cstreams, defStreamEncoder())
}

// defStreamEncoder returns the default stream encoder. Typically FlateEncoder, although RawEncoder
// can be useful for debugging.
func defStreamEncoder() core.StreamEncoder {
	return core.NewFlateEncoder()
}

// genFieldSignatureAppearance generates the appearance dictionary for a
// signature appearance widget.
func genFieldSignatureAppearance(fields []*SignatureLine, opts *SignatureFieldOpts) (*core.PdfObjectDictionary, error) {
	if opts == nil {
		opts = NewSignatureFieldOpts()
	}

	// Get font.
	var err error
	var fontName *core.PdfObjectName
	font := opts.Font

	if font != nil {
		descriptor, _ := font.GetFontDescriptor()
		if descriptor != nil {
			if f, ok := descriptor.FontName.(*core.PdfObjectName); ok {
				fontName = f
			}
		}
		if fontName == nil {
			fontName = core.MakeName("Font1")
		}
	} else {
		if font, err = model.NewStandard14Font("Helvetica"); err != nil {
			return nil, err
		}
		fontName = core.MakeName("Helv")
	}

	// Get font size and line height.
	fontSize := opts.FontSize
	if fontSize <= 0 {
		fontSize = 10
	}

	if opts.LineHeight <= 0 {
		opts.LineHeight = 1
	}
	lineHeight := opts.LineHeight * fontSize

	// Get space character width.
	spaceMetrics, found := font.GetRuneMetrics(' ')
	if !found {
		return nil, errors.New("the font does not have a space glyph")
	}
	spaceWidth := spaceMetrics.Wx

	// Generate lines.
	var maxLineWidth float64
	var lines []string

	for _, field := range fields {
		if field.Text == "" {
			continue
		}

		line := field.Text
		if field.Desc != "" {
			line = field.Desc + ": " + line
		}
		lines = append(lines, line)

		var lineWidth float64
		for _, r := range line {
			metrics, has := font.GetRuneMetrics(r)
			if !has {
				continue
			}

			lineWidth += metrics.Wx
		}

		if lineWidth > maxLineWidth {
			maxLineWidth = lineWidth
		}
	}

	maxLineWidth = maxLineWidth * fontSize / 1000.0
	height := float64(len(lines)) * lineHeight

	// Calculate annotation rectangle.
	rect := opts.Rect
	if rect == nil {
		rect = []float64{0, 0, maxLineWidth, height}
		opts.Rect = rect
	}
	rectWidth := rect[2] - rect[0]
	rectHeight := rect[3] - rect[1]

	// Fit contents.
	var offsetY float64
	if opts.AutoSize {
		if maxLineWidth > rectWidth || height > rectHeight {
			scale := math.Min(rectWidth/maxLineWidth, rectHeight/height)
			fontSize *= scale
		}

		lineHeight = opts.LineHeight * fontSize
		offsetY += (rectHeight - float64(len(lines))*lineHeight) / 2
	}

	// Draw annotation rectangle.
	cc := contentstream.NewContentCreator()

	if opts.BorderSize <= 0 {
		opts.BorderSize = 0
		opts.BorderColor = model.NewPdfColorDeviceGray(1)
	}
	if opts.BorderColor == nil {
		opts.FillColor = model.NewPdfColorDeviceGray(1)
	}
	if opts.FillColor == nil {
		opts.FillColor = model.NewPdfColorDeviceGray(1)
	}

	cc.Add_q().
		Add_re(rect[0], rect[1], rectWidth, rectHeight).
		Add_w(opts.BorderSize).
		SetStrokingColor(opts.BorderColor).
		SetNonStrokingColor(opts.FillColor).
		Add_B().
		Add_Q()

	// Draw signature.
	cc.Add_q()
	cc.Translate(rect[0], rect[3]-lineHeight-offsetY)
	cc.Add_BT()

	encoder := font.Encoder()
	for _, line := range lines {
		var encStr []byte
		for _, r := range line {
			if unicode.IsSpace(r) {
				if len(encStr) > 0 {
					cc.SetNonStrokingColor(opts.TextColor).
						Add_Tf(*fontName, fontSize).
						Add_TL(lineHeight).
						Add_TJ([]core.PdfObject{core.MakeStringFromBytes(encStr)}...)
					encStr = nil
				}

				cc.Add_Tf(*fontName, fontSize).
					Add_TL(lineHeight).
					Add_TJ([]core.PdfObject{core.MakeFloat(-spaceWidth)}...)
			} else {
				encStr = append(encStr, encoder.Encode(string(r))...)
			}
		}

		if len(encStr) > 0 {
			cc.SetNonStrokingColor(opts.TextColor).
				Add_Tf(*fontName, fontSize).
				Add_TL(lineHeight).
				Add_TJ([]core.PdfObject{core.MakeStringFromBytes(encStr)}...)
		}

		cc.Add_Td(0, -lineHeight)
	}

	cc.Add_ET()
	cc.Add_Q()

	// Create appearance dictionary.
	resources := model.NewPdfPageResources()
	resources.SetFontByName(*fontName, font.ToPdfObject())

	xform := model.NewXObjectForm()
	xform.Resources = resources
	xform.BBox = core.MakeArrayFromFloats(rect)
	xform.SetContentStream(cc.Bytes(), defStreamEncoder())

	apDict := core.MakeDict()
	apDict.Set("N", xform.ToPdfObject())
	return apDict, nil
}
