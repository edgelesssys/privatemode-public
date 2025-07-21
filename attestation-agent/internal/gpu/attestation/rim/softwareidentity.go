package rim

import "encoding/xml"

// SoftwareIdentity is an ISO-IEC 19770-2 Software Identification document as returned by the NVIDIA RIM service.
type SoftwareIdentity struct {
	XMLName      xml.Name `xml:"SoftwareIdentity"`
	Text         string   `xml:",chardata"`
	Xmlns        string   `xml:"xmlns,attr"`
	Ns0          string   `xml:"ns0,attr"`
	Ns2          string   `xml:"ns2,attr"`
	Corpus       string   `xml:"corpus,attr"`
	Name         string   `xml:"name,attr"`
	Patch        string   `xml:"patch,attr"`
	Supplemental string   `xml:"supplemental,attr"`
	TagID        string   `xml:"tagId,attr"`
	Version      string   `xml:"version,attr"`
	TagVersion   string   `xml:"tagVersion,attr"`
	Entity       struct {
		Text string `xml:",chardata"`
		Name string `xml:"name,attr"`
		Role string `xml:"role,attr"`
	} `xml:"Entity"`
	Meta struct {
		Text                    string `xml:",chardata"`
		Ns1                     string `xml:"ns1,attr"`
		ColloquialVersion       string `xml:"colloquialVersion,attr"`
		Edition                 string `xml:"edition,attr"`
		Product                 string `xml:"product,attr"`
		Revision                string `xml:"revision,attr"`
		PayloadType             string `xml:"PayloadType,attr"`
		BindingSpec             string `xml:"BindingSpec,attr"`
		BindingSpecVersion      string `xml:"BindingSpecVersion,attr"`
		PlatformManufacturerID  string `xml:"PlatformManufacturerId,attr"`
		PlatformManufacturerStr string `xml:"PlatformManufacturerStr,attr"`
		PlatformModel           string `xml:"PlatformModel,attr"`
		FirmwareManufacturer    string `xml:"FirmwareManufacturer,attr"`
		FirmwareManufacturerID  string `xml:"FirmwareManufacturerId,attr"`
	} `xml:"Meta"`
	Payload struct {
		Text     string `xml:",chardata"`
		SHA384   string `xml:"SHA384,attr"`
		Resource []struct {
			Text         string `xml:",chardata"`
			Type         string `xml:"type,attr"`
			Index        string `xml:"index,attr"`
			Active       string `xml:"active,attr"`
			Alternatives string `xml:"alternatives,attr"`
			Hash0        string `xml:"Hash0,attr"`
			Name         string `xml:"name,attr"`
			Size         string `xml:"size,attr"`
			Hash1        string `xml:"Hash1,attr"`
			Hash2        string `xml:"Hash2,attr"`
			Hash3        string `xml:"Hash3,attr"`
		} `xml:"Resource"`
	} `xml:"Payload"`
	Signature struct {
		Text       string `xml:",chardata"`
		Ds         string `xml:"ds,attr"`
		SignedInfo struct {
			Text                   string `xml:",chardata"`
			CanonicalizationMethod struct {
				Text      string `xml:",chardata"`
				Algorithm string `xml:"Algorithm,attr"`
			} `xml:"CanonicalizationMethod"`
			SignatureMethod struct {
				Text      string `xml:",chardata"`
				Algorithm string `xml:"Algorithm,attr"`
			} `xml:"SignatureMethod"`
			Reference struct {
				Text       string `xml:",chardata"`
				URI        string `xml:"URI,attr"`
				Transforms struct {
					Text      string `xml:",chardata"`
					Transform []struct {
						Text      string `xml:",chardata"`
						Algorithm string `xml:"Algorithm,attr"`
					} `xml:"Transform"`
				} `xml:"Transforms"`
				DigestMethod struct {
					Text      string `xml:",chardata"`
					Algorithm string `xml:"Algorithm,attr"`
				} `xml:"DigestMethod"`
				DigestValue string `xml:"DigestValue"`
			} `xml:"Reference"`
		} `xml:"SignedInfo"`
		SignatureValue string `xml:"SignatureValue"`
		KeyInfo        struct {
			Text     string `xml:",chardata"`
			X509Data struct {
				Text            string   `xml:",chardata"`
				X509Certificate []string `xml:"X509Certificate"`
			} `xml:"X509Data"`
		} `xml:"KeyInfo"`
	} `xml:"Signature"`
}
