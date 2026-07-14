package filepreview

import (
	"bytes"
	"image"
	"log/slog"

	"github.com/disintegration/imaging"
	"github.com/rwcarlsen/goexif/exif"
)

// prepareImageForPreview handles the complete image preparation pipeline
func prepareImageForPreview(data []byte) (image.Image, int, int, error) {
	imgReader := bytes.NewReader(data)

	img, _, err := image.Decode(imgReader)
	if err != nil {
		return nil, 0, 0, err
	}

	// Store original dimensions
	originalWidth := img.Bounds().Dx()
	originalHeight := img.Bounds().Dy()

	// Adjust orientation based on EXIF data
	exifReader := bytes.NewReader(data)
	img = adjustImageOrientation(exifReader, img)

	// Limit resolution to 1080p
	img = limitImageResolution(img, originalWidth, originalHeight)

	return img, originalWidth, originalHeight, nil
}

// limitImageResolution limits image resolution to 1080p while maintaining aspect ratio
func limitImageResolution(img image.Image, originalWidth, originalHeight int) image.Image {
	const maxImageWidth = 1920
	const maxImageHeight = 1080

	// Only resize if the image is larger than 1080p
	if originalWidth > maxImageWidth || originalHeight > maxImageHeight {
		resizedImg := imaging.Fit(img, maxImageWidth, maxImageHeight, imaging.Lanczos)
		return resizedImg
	}

	return img
}

// adjustImageOrientation adjusts image orientation based on EXIF data
func adjustImageOrientation(r *bytes.Reader, img image.Image) image.Image {
	exifData, err := exif.Decode(r)
	if err != nil {
		slog.Error("exif error", "error", err)
		return img
	}
	tag, err := exifData.Get(exif.Orientation)
	if err != nil {
		slog.Error("exif orientation error", "error", err)
		return img
	}
	orientation, err := tag.Int(0)
	if err != nil {
		slog.Error("exif orientation value error", "error", err)
		return img
	}
	return adjustOrientation(img, orientation)
}

// adjustOrientation applies the specified orientation transformation to the image
func adjustOrientation(img image.Image, orientation int) image.Image {
	switch orientation {
	case 1:
		return img
	case 2: //nolint:mnd // EXIF orientation: horizontal flip
		return imaging.FlipH(img)
	case 3: //nolint:mnd // EXIF orientation: 180 rotation
		return imaging.Rotate180(img)
	case 4: //nolint:mnd // EXIF orientation: vertical flip
		return imaging.FlipV(img)
	case 5: //nolint:mnd // EXIF orientation: transpose
		return imaging.Transpose(img)
	case 6: //nolint:mnd // EXIF orientation: 270 rotation
		return imaging.Rotate270(img)
	case 7: //nolint:mnd // EXIF orientation: transverse
		return imaging.Transverse(img)
	case 8: //nolint:mnd // EXIF orientation: 90 rotation
		return imaging.Rotate90(img)
	default:
		slog.Error("Invalid orientation value", "error", orientation)
		return img
	}
}

// resizeForANSIDithered resizes an image to the exact terminal cell grid
// (maxWidth × maxHeight) for the Floyd-Steinberg dithered ANSI renderer.
// Each cell maps to one source pixel. Lanczos + unsharp mask gives crisp
// output at low terminal resolutions.
func resizeForANSIDithered(img image.Image, maxWidth, maxHeight int) image.Image {
	resized := imaging.Fit(img, maxWidth, maxHeight, imaging.Lanczos)
	// Unsharp mask (40/100) keeps edges defined at low resolution.
	return imaging.Sharpen(resized, 40.0)
}
