package rim

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"strings"
)

// Resource is a direct reference measurement for a RIM bundle.
type Resource struct {
	Type         string   `xml:"type,attr"`
	Index        uint8    `xml:"index,attr"`
	Active       bool     `xml:"active,attr"`
	Alternatives int      `xml:"alternatives,attr"`
	Name         string   `xml:"name,attr"`
	Size         int      `xml:"size,attr"`
	Hashes       []string `xml:"hash,attr"`
}

// UnmarshalXML implements the [xml.Unmarshaler] interface.
func (r *Resource) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var tmp struct {
		Type         string     `xml:"type,attr"`
		Index        uint8      `xml:"index,attr"`
		Active       bool       `xml:"active,attr"`
		Alternatives int        `xml:"alternatives,attr"`
		Name         string     `xml:"name,attr"`
		Size         int        `xml:"size,attr"`
		Attr         []xml.Attr `xml:",any,attr"`
	}
	if err := d.DecodeElement(&tmp, &start); err != nil {
		return err
	}

	r.Type = tmp.Type
	r.Index = tmp.Index
	r.Active = tmp.Active
	r.Alternatives = tmp.Alternatives
	r.Name = tmp.Name
	r.Size = tmp.Size

	r.Hashes = make([]string, 0)

	for _, attr := range tmp.Attr {
		if strings.HasPrefix(attr.Name.Local, "Hash") {
			r.Hashes = append(r.Hashes, attr.Value)
		}
	}

	return nil
}

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
		Text     string     `xml:",chardata"`
		SHA384   string     `xml:"SHA384,attr"`
		Resource []Resource `xml:"Resource"`
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

// SigningCerts returns the certificate chain used to sign the SoftwareIdentity document.
func (s SoftwareIdentity) SigningCerts() ([]*x509.Certificate, error) {
	certs := make([]*x509.Certificate, len(s.Signature.KeyInfo.X509Data.X509Certificate))
	for i, certPEM := range s.Signature.KeyInfo.X509Data.X509Certificate {
		certDER, err := base64.StdEncoding.DecodeString(certPEM)
		if err != nil {
			return nil, fmt.Errorf("decoding signing certificate: %w", err)
		}
		cert, err := x509.ParseCertificate(certDER)
		if err != nil {
			return nil, fmt.Errorf("parsing signing certificate: %w", err)
		}
		certs[i] = cert
	}
	return certs, nil
}
