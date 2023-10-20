package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"path/filepath"
	"strconv"
	"strings"
)

func loadZones() map[string]map[string]interface{} {
	jsonZone := make(map[string]map[string]interface{})
	zoneFiles, _ := filepath.Glob("zones/*.zone")

	for _, zone := range zoneFiles {
		zonedata, err := ioutil.ReadFile(zone)
		if err != nil {
			fmt.Println("Error reading zone file:", err)
			continue
		}
		var data map[string]interface{}
		if err := json.Unmarshal(zonedata, &data); err != nil {
			fmt.Println("Error parsing zone file:", err)
			continue
		}
		zonename := data["$origin"].(string)
		jsonZone[zonename] = data
	}

	return jsonZone
}

func getFlags(flags []byte) []byte {
	QR := byte(0x80)
	OPCODE := flags[0] >> 3
	AA := byte(0x04)
	TC := byte(0x02)
	RD := byte(0x01)

	RA := byte(0x80)
	Z := byte(0)
	RCODE := byte(0)

	flagsByte := []byte{QR | OPCODE | AA | TC | RD, RA | Z | RCODE}
	return flagsByte
}

func getQuestionDomain(data []byte) ([]string, []byte) {
	state := 0
	expectedLength := byte(0)
	domainString := ""
	domainParts := []string{}
	x := 0
	y := 0

	for _, b := range data {
		if state == 1 {
			if b != 0 {
				domainString += string(b)
			}
			x++
			if x == int(expectedLength) {
				domainParts = append(domainParts, domainString)
				domainString = ""
				state = 0
				x = 0
			}
			if b == 0 {
				domainParts = append(domainParts, domainString)
				break
			}
		} else {
			state = 1
			expectedLength = b
		}
		y++
	}

	questionType := data[y : y+2]
	return domainParts, questionType
}

func getZone(domain []string) map[string]interface{} {
	zoneName := strings.Join(domain, ".")
	zonedata := loadZones()
	return zonedata[zoneName]
}

func getRecs(data []byte) ([]map[string]interface{}, string, []string) {
	domain, questionType := getQuestionDomain(data)
	qt := ""
	if bytes.Equal(questionType, []byte{0x00, 0x01}) {
		qt = "a"
	}

	zone := getZone(domain)

	if zone != nil {
		records, ok := zone[qt].([]map[string]interface{})
		if ok {
			return records, qt, domain
		}
	}

	return nil, "", []string{}
}

func buildQuestion(domainName []string, rectype string) []byte {
	qbytes := []byte{}

	for _, part := range domainName {
		length := byte(len(part))
		qbytes = append(qbytes, length)

		for _, char := range part {
			qbytes = append(qbytes, byte(char))
		}
	}

	if rectype == "a" {
		qbytes = append(qbytes, 0x00, 0x01)
	}

	qbytes = append(qbytes, 0x00, 0x01)
	return qbytes
}

func rectoBytes(rectype string, recttl uint32, recval string) []byte {
	rbytes := []byte{0xc0, 0x0c}

	if rectype == "a" {
		rbytes = append(rbytes, 0x00, 0x01)
	}

	rbytes = append(rbytes, 0x00, 0x01)

	ttlBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(ttlBytes, recttl)
	rbytes = append(rbytes, ttlBytes...)

	if rectype == "a" {
		for _, part := range strings.Split(recval, ".") {
			value, _ := strconv.Atoi(part)
			rbytes = append(rbytes, byte(value))
		}
	}
	return rbytes
}

func buildResponse(data []byte) []byte {
	transactionID := data[:2]

	flags := getFlags(data[2:4])

	qdCount := []byte{0x00, 0x01}

	records, _, _ := getRecs(data[12:])
	anCount := make([]byte, 2)
	binary.BigEndian.PutUint16(anCount, uint16(len(records)))

	nsCount := []byte{0x00, 0x00}

	arCount := []byte{0x00, 0x00}

	dnsHeader := append(transactionID, flags...)
	dnsHeader = append(dnsHeader, qdCount...)
	dnsHeader = append(dnsHeader, anCount...)
	dnsHeader = append(dnsHeader, nsCount...)
	dnsHeader = append(dnsHeader, arCount...)

	dnsBody := []byte{}

	records, rectype, domainName := getRecs(data[12:])

	dnsQuestion := buildQuestion(domainName, rectype)

	for _, record := range records {
		dnsBody = append(dnsBody, rectoBytes(rectype, uint32(record["ttl"].(float64)), record["value"].(string))...)
	}

	return append(dnsHeader, append(dnsQuestion, dnsBody...)...)
}

func main() {
	udpAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:53")
	if err != nil {
		fmt.Println("Error resolving UDP address:", err)
		return
	}

	sock, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		fmt.Println("Error listening on UDP:", err)
		return
	}
	defer sock.Close()

	for {
		buffer := make([]byte, 512)
		n, addr, err := sock.ReadFromUDP(buffer)
		if err != nil {
			fmt.Println("Error reading from UDP:", err)
			continue
		}
		data := buffer[:n]
		response := buildResponse(data)
		_, err = sock.WriteToUDP(response, addr)
		if err != nil {
			fmt.Println("Error writing to UDP:", err)
		}
	}
}
