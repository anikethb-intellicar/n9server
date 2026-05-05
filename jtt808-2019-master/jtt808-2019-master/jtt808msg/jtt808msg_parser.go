package jtt808msg

import "github.com/fabrikiot/goutils/beutils"

func skipTillValidMsg(data []byte, startPos int, maxPos int) int {
	for i := startPos; i < maxPos; i++ {
		if data[i] == 0x7E {
			return i
		}
	}
	return -1
}

func extractJTT808Msg(b []byte, startPos int, maxPos int) *JTT808Msg {
	header := make([]byte, 0, 21)
	messagebody := make([]byte, 0, maxPos-startPos)

	// 1. First read the header...
	currIdx := startPos + 1
	for {
		if len(header) == 17 {
			break
		}

		if currIdx > maxPos {
			return nil
		}
		if b[currIdx] == 0x7D {
			if currIdx+1 > maxPos {
				return nil
			}
			if b[currIdx+1] == 0x01 {
				header = append(header, 0x7D)
			} else if b[currIdx+1] == 0x02 {
				header = append(header, 0x7E)
			} else {
				return nil
			}
			currIdx += 2
		} else {
			header = append(header, b[currIdx])
			currIdx++
		}
	}
	// 1.1 We have read the header now, we will check if we have Encapsulation item...
	// The properties word has the details about the encapsulation...
	headerPropU16, _, _ := beutils.ReadU16(header, 2)
	msgBodyLen := headerPropU16 & 0x03FF
	isMultiplePkgs := headerPropU16 & 0x2000
	if isMultiplePkgs == 0x2000 {
		// Read four more bytes..
		for {
			if len(header) == 21 {
				break
			}

			if currIdx > maxPos {
				return nil
			}

			if b[currIdx] == 0x7D {
				if currIdx+1 > maxPos {
					return nil
				}
				if b[currIdx+1] == 0x01 {
					header = append(header, 0x7D)
				} else if b[currIdx+1] == 0x02 {
					header = append(header, 0x7E)
				} else {
					return nil
				}
				currIdx += 2
			} else {
				header = append(header, b[currIdx])
				currIdx++
			}
		}
	}

	// 2. Now read the message body after unescaping...
	if currIdx+int(msgBodyLen) > maxPos {
		return nil
	}

	for {
		if len(messagebody) == int(msgBodyLen) {
			break
		}
		if currIdx > maxPos {
			return nil
		}
		if b[currIdx] == 0x7D {
			if currIdx+1 > maxPos {
				return nil
			}
			if b[currIdx+1] == 0x01 {
				messagebody = append(messagebody, 0x7D)
			} else if b[currIdx+1] == 0x02 {
				messagebody = append(messagebody, 0x7E)
			} else {
				return nil
			}
			currIdx += 2
		} else {
			messagebody = append(messagebody, b[currIdx])
			currIdx++
		}
	}

	return &JTT808Msg{
		Header:      header,
		MessageBody: messagebody,
	}
}

// This will check the given byte array and return the next beacon if it is fully available, otherwise it will return nil for the beacon,
// It will also return the position till which you can skip safely with or without the beacon..
// Returns:
// int => the position till which you can safely drop from the buffer..
// beacon if it is available...
func GetNextJTT808Msg(b []byte, startPos int, maxPos int) (int, *JTT808Msg) {
	skipTillPos := startPos
	for {
		nextValidPos := skipTillValidMsg(b, skipTillPos, maxPos)
		if nextValidPos == -1 {
			return maxPos, nil
		}

		if nextValidPos+16 > maxPos {
			return nextValidPos, nil
		}

		// I Probably have atleast the header here...
		// If this is not the case, then it means we have found 0x7E.. we will iterate till we reach the next 0x7E..
		// Along with that we will try to do the checksum so that if there is a valid beacon we can return it...
		currIdx := nextValidPos + 1
		checksum := uint8(0)
		isErrBeaconFound := false
		for i := currIdx; i < (maxPos - 1); i++ {
			checksum ^= b[i]
			if b[i+1] == 0x7E {
				if checksum == 0 {
					// Checksum is good to go.. we should extract the beacon from the start to this point...
					jtt808Msg := extractJTT808Msg(b, nextValidPos, i+2)
					if jtt808Msg != nil {
						return i + 2, jtt808Msg
					}
				}
				skipTillPos = nextValidPos + 1
				isErrBeaconFound = true
				break
			}
		}
		if !isErrBeaconFound {
			// This basically means that the full beacon is not yet available...
			return nextValidPos, nil
		}
	}
}
