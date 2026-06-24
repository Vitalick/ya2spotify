package spotifyconnect

import "golang.org/x/text/encoding/charmap"

// DecodeWindows1251 декодирует байты из Windows-1251 в UTF-8.
//
// Parameters:
//   - ba: исходные байты в кодировке Windows-1251.
//
// Returns:
//   - []uint8: результат декодирования; при ошибке возвращается частично декодированное значение.
func DecodeWindows1251(ba []uint8) []uint8 {
	dec := charmap.Windows1251.NewDecoder()
	out, _ := dec.Bytes(ba)
	return out
}

// EncodeWindows1251 кодирует UTF-8 байты в Windows-1251.
//
// Parameters:
//   - ba: исходные UTF-8 байты.
//
// Returns:
//   - []uint8: результат кодирования; при ошибке возвращается частично закодированное значение.
func EncodeWindows1251(ba []uint8) []uint8 {
	enc := charmap.Windows1251.NewEncoder()
	out, _ := enc.String(string(ba))
	return []uint8(out)
}
