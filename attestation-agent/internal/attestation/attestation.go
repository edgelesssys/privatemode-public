/*
Package attestation implements issuer/verifier based attestation of
NVIDIA GPUs.
*/
package attestation

import (
	"crypto/ecdsa"
	"crypto/sha512"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strings"

	"github.com/edgelesssys/continuum/attestation-agent/internal/rim"
)

// OpaqueFieldID is the ID of an opaque field in the attestation report.
type OpaqueFieldID uint16

// OpaqueFieldID constants for fields in the attestation report.
const (
	OpaqueFieldIDCertIssuerName OpaqueFieldID = iota + 1
	OpaqueFieldIDCertAuthorityKeyIdentifier
	OpaqueFieldIDDriverVersion
	OpaqueFieldIDGpuInfo
	OpaqueFieldIDSku
	OpaqueFieldIDVbiosVersion
	OpaqueFieldIDManufacturerID
	OpaqueFieldIDTamperDetection
	OpaqueFieldIDSmc
	OpaqueFieldIDVpr
	OpaqueFieldIDNvdec0Status
	OpaqueFieldIDMsrscnt
	OpaqueFieldIDCprInfo
	OpaqueFieldIDBoardID
	OpaqueFieldIDChipSku
	OpaqueFieldIDChipSkuMod
	OpaqueFieldIDProject
	OpaqueFieldIDProjectSku
	OpaqueFieldIDProjectSkuMod
	OpaqueFieldIDFwid
	OpaqueFieldIDProtectedPcieStatus
	OpaqueFieldIDSwitchPdi
	OpaqueFieldIDFloorsweptPorts
	OpaqueFieldIDPositionID
	OpaqueFieldIDLockSwitchStatus
	OpaqueFieldIDGpuLinkConn
	OpaqueFieldIDSysEnableStatus
	OpaqueFieldIDOpaqueDataVersion
	OpaqueFieldIDInvalid OpaqueFieldID = 255
)

const (
	// dmtfMeasurementSpecification is a magic value indicating a DMTF measurement.
	// See: https://github.com/NVIDIA/nvtrust/blob/d0d6536e8e52c53f5302d15db6074ff66d36adeb/guest_tools/gpu_verifiers/local_gpu_verifier/src/verifier/attestation/spdm_msrt_resp_msg.py#L162
	dmtfMeasurementSpecification                = 0x01
	spdmGetRequestMeasurementRequestMessageSize = 37
	signatureLength                             = 96
)

// VerificationSettings holds the configuration to verify a GPU attestation report.
type VerificationSettings struct {
	// CertChain is the certificate chain used to verify the report signature.
	// The leaf certificate is expected to be the signing certificate of the report.
	// The certificate chain is expected to be trusted and verified beforehand.
	CertChain []*x509.Certificate
	// Nonce that was used to generate the report.
	Nonce [32]byte
	// AllowedDriverVersions are the driver versions the GPU is allowed to run with.
	AllowedDriverVersions []string
	// AllowedVBIOSVersions are the VBIOS versions the GPU is allowed to run with.
	AllowedVBIOSVersions []string
}

// DMTFMeasurement of the report.
type DMTFMeasurement struct {
	ValueType uint8
	Value     []byte
}

// MeasurementRecord holds a single measurement record in the attestation report.
type MeasurementRecord struct {
	Index       uint8
	MrSpec      uint8
	Measurement DMTFMeasurement
}

// OpaqueData of the NVIDIA Hopper GPU attestation report.
// The structure of the data in this field is as follows:
// [DataType(2 bytes)|DataSize(2 bytes)|Data(DataSize bytes)][DataType(2 bytes)|DataSize(2 bytes)|Data(DataSize bytes)]...
type OpaqueData struct {
	MeasurementCount []uint32
	Fields           map[OpaqueFieldID]any
}

// SPDMMeasurementRequestMessage is the parsed SPDM measurement request message from an attestation report.
type SPDMMeasurementRequestMessage struct {
	SPDMVersion         uint8
	RequestResponseCode uint8
	Param1              uint8
	Param2              uint8
	Nonce               [32]byte
	SlotIDParam         uint8
}

// SPDMMeasurementResponseMessage is the parsed SPDM measurement response message from an attestation report.
type SPDMMeasurementResponseMessage struct {
	SPDMVersion             uint8
	RequestResponseCode     uint8
	Param1                  uint8
	Param2                  uint8
	NumberOfBlocks          uint8
	MeasurementRecordLength uint32 // In Reality, this is a 3-byte value ¯\_(ツ)_/¯
	MeasurementRecords      map[uint8]MeasurementRecord
	Nonce                   [32]byte
	OpaqueData              OpaqueData
	Signature               []byte
}

// Report holds a parsed GPU attestation report.
type Report struct {
	RequestData  []byte
	ResponseData []byte
	SPDMRequest  SPDMMeasurementRequestMessage
	SPDMResponse SPDMMeasurementResponseMessage
}

// ParseReport parses a GPU attestation report from a byte slice.
func ParseReport(data []byte) (*Report, error) {
	var report Report

	if len(data) <= spdmGetRequestMeasurementRequestMessageSize {
		return nil, fmt.Errorf("invalid report: more than %d bytes required for request data, got %d", spdmGetRequestMeasurementRequestMessageSize, len(data))
	}

	report.RequestData = make([]byte, spdmGetRequestMeasurementRequestMessageSize)
	copy(report.RequestData, data[:spdmGetRequestMeasurementRequestMessageSize])

	report.ResponseData = make([]byte, len(data)-spdmGetRequestMeasurementRequestMessageSize)
	copy(report.ResponseData, data[spdmGetRequestMeasurementRequestMessageSize:])

	spdmRequest, err := parseSPDMRequest(report.RequestData)
	if err != nil {
		return nil, err
	}
	report.SPDMRequest = spdmRequest

	spdmResponse, err := parseSPDMResponse(report.ResponseData)
	if err != nil {
		return nil, err
	}
	report.SPDMResponse = spdmResponse

	return &report, nil
}

// DriverVersion returns the driver version of the report.
func (r *Report) DriverVersion() string {
	return opaqueDataToString(r.SPDMResponse.OpaqueData.Fields[OpaqueFieldIDDriverVersion])
}

// Project returns the project name.
func (r *Report) Project() string {
	return opaqueDataToString(r.SPDMResponse.OpaqueData.Fields[OpaqueFieldIDProject])
}

// ProjectSKU returns the project SKU.
func (r *Report) ProjectSKU() string {
	return opaqueDataToString(r.SPDMResponse.OpaqueData.Fields[OpaqueFieldIDProjectSku])
}

// ChipSKU returns the chip SKU.
func (r *Report) ChipSKU() string {
	return opaqueDataToString(r.SPDMResponse.OpaqueData.Fields[OpaqueFieldIDChipSku])
}

// VBIOSVersion returns the VBIOS version in the format "XX.XX.XX.XX".
func (r *Report) VBIOSVersion() (string, error) {
	opaqueFieldVBIOS, ok := r.SPDMResponse.OpaqueData.Fields[OpaqueFieldIDVbiosVersion].([]byte)
	if !ok {
		return "", fmt.Errorf("invalid vbios version format")
	}
	vbiosSlice := make([]byte, len(opaqueFieldVBIOS))
	copy(vbiosSlice, opaqueFieldVBIOS)
	slices.Reverse(vbiosSlice)
	vbiosHex := hex.EncodeToString(vbiosSlice)

	tmp := vbiosHex[len(vbiosHex)/2:] + vbiosHex[len(vbiosHex)/2-2:len(vbiosHex)/2]

	idx := 0
	var vbios string
	for i := 0; i < len(tmp)-2; i += 2 {
		vbios = vbios + tmp[i:i+2] + "."
		idx = i + 2
	}
	vbios = vbios + tmp[idx:idx+2]

	return vbios, nil
}

// Verify checks the report against the provided settings and verifies the signature.
func (r *Report) Verify(settings VerificationSettings) error {
	if r.SPDMRequest.Nonce != settings.Nonce {
		return fmt.Errorf("nonce mismatch: expected %x, got %x", settings.Nonce, r.SPDMRequest.Nonce)
	}

	if !slices.Contains(settings.AllowedDriverVersions, strings.ToUpper(r.DriverVersion())) {
		return fmt.Errorf("driver version mismatch: expected one of %s, got %q", settings.AllowedDriverVersions, r.DriverVersion())
	}

	vbiosVersion, err := r.VBIOSVersion()
	if err != nil {
		return fmt.Errorf("getting VBIOS version: %w", err)
	}
	if !slices.Contains(settings.AllowedVBIOSVersions, strings.ToUpper(vbiosVersion)) {
		return fmt.Errorf("VBIOS version mismatch: expected one of %s, got %q", settings.AllowedVBIOSVersions, vbiosVersion)
	}

	if err := r.verifySignature(settings.CertChain[0]); err != nil {
		return fmt.Errorf("verifying report signature: %w", err)
	}
	return nil
}

// ValidateMeasurements validates the measurements in the report against the provided reference measurements.
func (r *Report) ValidateMeasurements(vbiosRefs, driverRefs *rim.SoftwareIdentity, allowedMismatches []uint8) error {
	goldenMeasurements, err := generateGoldenMeasurements(vbiosRefs, driverRefs)
	if err != nil {
		return fmt.Errorf("parsing reference measurements: %w", err)
	}

	if len(goldenMeasurements) > len(r.SPDMResponse.MeasurementRecords) {
		return errors.New("received more reference measurements than available measurements in the report")
	}

	var errs []error
	for idx, goldenMeasurement := range goldenMeasurements {
		if slices.Contains(allowedMismatches, idx) {
			continue
		}
		reportedMeasurement, ok := r.SPDMResponse.MeasurementRecords[idx]
		if !ok {
			errs = append(errs, fmt.Errorf("missing measurement record for index %d", idx))
			continue
		}

		matched := false
		for _, referenceMeasurement := range goldenMeasurement {
			if strings.EqualFold(hex.EncodeToString(reportedMeasurement.Measurement.Value), referenceMeasurement) {
				matched = true
				break
			}
		}
		if !matched {
			errs = append(errs,
				fmt.Errorf("no matching measurement found in %s for index %d: %s",
					goldenMeasurement, idx, hex.EncodeToString(reportedMeasurement.Measurement.Value),
				))
		}
	}

	return errors.Join(errs...)
}

// verifySignature checks the signature of the report against the provided signing certificate.
func (r *Report) verifySignature(signingCert *x509.Certificate) error {
	if len(r.ResponseData) < signatureLength {
		return fmt.Errorf("invalid report: response data too short, expected at least %d bytes, got %d", signatureLength, len(r.ResponseData))
	}

	toVerify := make([]byte, len(r.RequestData)+len(r.ResponseData)-signatureLength)
	copy(toVerify, r.RequestData)
	copy(toVerify[len(r.RequestData):], r.ResponseData[:len(r.ResponseData)-signatureLength])
	digest := sha512.Sum384(toVerify)

	signature := r.SPDMResponse.Signature
	sigR := big.NewInt(0).SetBytes(signature[:len(signature)/2])
	sigS := big.NewInt(0).SetBytes(signature[len(signature)/2:])

	pubKey, ok := signingCert.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return fmt.Errorf("invalid public key type: expected ecdsa.PublicKey, got %T", signingCert.PublicKey)
	}
	if !ecdsa.Verify(pubKey, digest[:], sigR, sigS) {
		return errors.New("ECDSA signature verification failed")
	}

	return nil
}

func parseOpaqueData(data []byte) (OpaqueData, error) {
	var od OpaqueData
	od.Fields = make(map[OpaqueFieldID]any)
	od.MeasurementCount = []uint32{}

	for i := 0; i < len(data); {
		dataType := OpaqueFieldID(binary.LittleEndian.Uint16(data[i : i+2]))
		dataSize := binary.LittleEndian.Uint16(data[i+2 : i+4])
		dataValue := data[i+4 : i+4+int(dataSize)]

		if dataType == OpaqueFieldIDMsrscnt {
			mc, err := parseMeasurementCount(dataValue)
			if err != nil {
				return OpaqueData{}, fmt.Errorf("parsing measurement count: %w", err)
			}
			od.MeasurementCount = mc
		}
		od.Fields[dataType] = data[i+4 : i+4+int(dataSize)]
		i += 4 + int(dataSize)
	}

	return od, nil
}

func parseMeasurementCount(data []byte) ([]uint32, error) {
	out := []uint32{}
	if len(data)%4 != 0 {
		return out, errors.New("invalid measurement count format")
	}

	for i := 0; i < len(data); i += 4 {
		out = append(out, binary.LittleEndian.Uint32(data[i:]))
	}

	return out, nil
}

func parseSPDMRequest(data []byte) (SPDMMeasurementRequestMessage, error) {
	if len(data) < spdmGetRequestMeasurementRequestMessageSize {
		return SPDMMeasurementRequestMessage{}, fmt.Errorf("invalid SPDM request data: expected at least %d bytes, got %d", spdmGetRequestMeasurementRequestMessageSize, len(data))
	}
	nonce := [32]byte{}
	copy(nonce[:], data[4:36])
	return SPDMMeasurementRequestMessage{
		SPDMVersion:         data[0],
		RequestResponseCode: data[1],
		Param1:              data[2],
		Param2:              data[3],
		Nonce:               nonce,
		SlotIDParam:         data[36],
	}, nil
}

func parseSPDMResponse(report []byte) (SPDMMeasurementResponseMessage, error) {
	// See https://www.dmtf.org/sites/default/files/standards/documents/DSP0274_1.3.0.pdf
	// Table 52: Successful MEASUREMENTS response message format
	reportLen := len(report)

	// Bytes 5 -> 8: Measurement Record Length
	if reportLen < 8 {
		return SPDMMeasurementResponseMessage{}, errors.New("report too short")
	}
	mrLength := binary.LittleEndian.Uint32([]byte{report[5], report[6], report[7], 0x00})
	idx := 8

	// Bytes 8 -> 8 + mrLength: Measurement Records
	measurementRecords, err := parseMeasurementRecords(report[idx : idx+int(mrLength)])
	if err != nil {
		return SPDMMeasurementResponseMessage{}, fmt.Errorf("parsing measurement records: %w", err)
	}
	idx += int(mrLength)

	// Bytes 8 + mrLength -> 8 + mrLength + 32: Nonce
	if reportLen < idx+32 {
		return SPDMMeasurementResponseMessage{}, fmt.Errorf("report too short for nonce, expected %d bytes, got %d", idx+32, reportLen)
	}
	nonce := [32]byte{}
	copy(nonce[:], report[idx:idx+32])
	idx += 32

	// Bytes 40 + mrLength -> 40 + mrLength + 2: Opaque Data Length
	if reportLen < idx+2 {
		return SPDMMeasurementResponseMessage{}, fmt.Errorf("report too short for opaque data length, expected %d bytes, got %d", idx+2, reportLen)
	}
	opaqueDataLength := int(binary.LittleEndian.Uint16(report[idx : idx+2]))
	idx += 2

	// Bytes 42 + mrLength -> 42 + mrLength + opaqueLength: Opaque Data
	if reportLen < idx+2+opaqueDataLength {
		return SPDMMeasurementResponseMessage{}, errors.New("report too short for opaque data")
	}
	opaqueData, err := parseOpaqueData(report[idx : idx+opaqueDataLength])
	if err != nil {
		return SPDMMeasurementResponseMessage{}, fmt.Errorf("parsing opaque data: %w", err)
	}
	idx += opaqueDataLength

	// Bytes 42 + mrLength + opaqueLength -> 42 + mrLength + opaqueLength + [signatureLength]: Signature
	if reportLen < idx+signatureLength {
		return SPDMMeasurementResponseMessage{}, fmt.Errorf("report too short for signature, expected %d bytes, got %d", idx+signatureLength, reportLen)
	}
	signature := make([]byte, signatureLength)
	copy(signature, report[idx:idx+signatureLength])

	return SPDMMeasurementResponseMessage{
		SPDMVersion:             report[0],
		RequestResponseCode:     report[1],
		Param1:                  report[2],
		Param2:                  report[3],
		NumberOfBlocks:          report[4],
		MeasurementRecordLength: mrLength,
		MeasurementRecords:      measurementRecords,
		Nonce:                   nonce,
		OpaqueData:              opaqueData,
		Signature:               signature,
	}, nil
}

func opaqueDataToString(i any) string {
	var ret string
	switch v := i.(type) {
	case []byte:
		ret = string(v)
	case string:
		ret = v
	default:
		ret = fmt.Sprintf("%v", v)
	}
	return strings.ToUpper(strings.Trim(strings.TrimSpace(ret), "\x00"))
}

// parseMeasurementRecords parses the measurement records from the SPDM response.
func parseMeasurementRecords(measurementRecordData []byte) (map[uint8]MeasurementRecord, error) {
	records := make(map[uint8]MeasurementRecord)

	for i := 0; i < len(measurementRecordData); {
		if len(measurementRecordData) < i+4 {
			return nil, fmt.Errorf("measurement record data too short at index %d", i)
		}

		// Enclosing measurement record
		mrSpec := measurementRecordData[i+1]
		if mrSpec != dmtfMeasurementSpecification {
			return nil, fmt.Errorf("measurement block %d not following DMTF specification", i)
		}

		mrLength := binary.LittleEndian.Uint16(measurementRecordData[i+2 : i+4])
		if len(measurementRecordData) < i+4+int(mrLength) {
			return nil, fmt.Errorf("measurement record %d length %d exceeds available data", i, mrLength)
		}

		// DMTF measurement
		dmtfMeasurementData := measurementRecordData[i+4 : i+4+int(mrLength)]

		dmtfMeasurementLength := binary.LittleEndian.Uint16(dmtfMeasurementData[1:3])
		if len(dmtfMeasurementData) < 3+int(dmtfMeasurementLength) {
			return nil, fmt.Errorf("DMTF measurement data length %d exceeds available data", dmtfMeasurementLength)
		}

		dmtfMeasurementValue := make([]byte, dmtfMeasurementLength)
		copy(dmtfMeasurementValue, dmtfMeasurementData[3:3+int(dmtfMeasurementLength)])

		dmtfMeasurement := DMTFMeasurement{
			ValueType: dmtfMeasurementData[0],
			Value:     dmtfMeasurementValue,
		}

		// Handle wraparound from Python implementation:
		// https://github.com/NVIDIA/nvtrust/blob/d0d6536e8e52c53f5302d15db6074ff66d36adeb/guest_tools/gpu_verifiers/local_gpu_verifier/src/verifier/attestation/spdm_msrt_resp_msg.py#L173
		idx := measurementRecordData[i]
		if idx != 0 {
			idx--
		}

		records[idx] = MeasurementRecord{
			Index:       idx,
			MrSpec:      measurementRecordData[i+1],
			Measurement: dmtfMeasurement,
		}

		i += 4 + int(mrLength)
	}

	return records, nil
}

func generateGoldenMeasurements(vbiosRefs, driverRefs *rim.SoftwareIdentity) (map[uint8][]string, error) {
	referenceMeasurements := make(map[uint8][]string)

	for _, resource := range vbiosRefs.Payload.Resource {
		if !resource.Active {
			continue
		}
		referenceMeasurements[resource.Index] = resource.Hashes
	}

	for _, resource := range driverRefs.Payload.Resource {
		if !resource.Active {
			continue
		}
		if _, ok := referenceMeasurements[resource.Index]; ok {
			return nil, fmt.Errorf("duplicate resource index %d found in reference measurements", resource.Index)
		}
		referenceMeasurements[resource.Index] = resource.Hashes
	}

	return referenceMeasurements, nil
}
