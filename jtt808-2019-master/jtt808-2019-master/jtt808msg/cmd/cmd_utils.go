package cmd

import "golang.org/x/text/encoding/simplifiedchinese"

func JTT808UtilsBCDToString(bcd []byte) []byte {
	result := make([]byte, 0, len(bcd)*2)

	for _, b := range bcd {
		high := (b >> 4) & 0x0F
		low := b & 0x0F

		if high <= 9 {
			result = append(result, '0'+high)
		}
		if low <= 9 {
			result = append(result, '0'+low)
		}
	}

	return result
}

func JTT808UtilsStringToBCD(str []byte) []byte {
	result := make([]byte, len(str)/2)

	for i := 0; i < len(str)/2; i++ {
		if i*2+1 < len(str) {
			high := str[i*2] - '0'
			low := str[i*2+1] - '0'
			if high > 9 {
				high = 0
			}
			if low > 9 {
				low = 0
			}
			result[i] = (high << 4) | low
		}
	}

	return result
}
func JTT808UtilsGBKToUtf8(gbk []byte) []byte {
	utf8, err := simplifiedchinese.GBK.NewDecoder().Bytes(gbk)
	if err != nil {
		return gbk
	}
	return utf8
}

func JTT808UtilsUtf8ToGBK(utf8 []byte) []byte {
	gbk, err := simplifiedchinese.GBK.NewEncoder().Bytes(utf8)
	if err != nil {
		return utf8
	}
	return gbk
}
