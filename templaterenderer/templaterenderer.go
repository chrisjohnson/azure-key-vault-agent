package templaterenderer

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"encoding/pem"
	"io/ioutil"
	"log"
	"text/template"

	"github.com/chrisjohnson/azure-key-vault-agent/resource"
	"github.com/Masterminds/sprig"
	"golang.org/x/crypto/pkcs12"
	"crypto/x509"
)

func RenderFile(path string, resourceMap resource.ResourceMap) string {
	contents, err := ioutil.ReadFile(path)

	if err != nil {
		log.Panicf("Error reading template %v: %v", path, err)
	}

	return RenderInline(string(contents), resourceMap)
}

func RenderInline(templateContents string, resourceMap resource.ResourceMap) string {
	helpers := template.FuncMap{
		"privateKey": func(name string) interface{} {
			value, ok := resourceMap.Secrets[name]
			privateKey := ""
			if ok {
				switch contentType := *value.ContentType; contentType {
				case "application/x-pem-file":
					privateKey = pemPrivateKeyFromPem(*value.Value)
				case "application/x-pkcs12":
					privateKey = pemPrivateKeyFromPkcs12(*value.Value)
				default:
					log.Panicf("Got unexpected content type: %v", contentType)
				}
			} else {
				log.Panicf("privateKey lookup failed: Expected a Secret with name %v\n", name)
			}
			return privateKey
		},
		"cert": func(name string) interface{} {
			value, ok := resourceMap.Secrets[name]
			// TODO: If the cert can be found on either a Cert or a Secret, we should handle discovering it from both
			if ok {
				// TODO: Transform value to extract the cert using some sort of library that can parse PEM format
				// TODO: How to handle PKCS12?
			} else {
				log.Panicf("cert lookup failed: Expected a Secret with name %v\n", name)
			}
			return value
		},
	}

	// Init the template
	t, err := template.New("template").Funcs(helpers).Funcs(sprig.TxtFuncMap()).Parse(templateContents)
	if err != nil {
		log.Panicf("Error parsing template:\n%v\nError:\n%v\n", templateContents, err)
	}

	// Execute the template
	var buf bytes.Buffer
	err = t.Execute(&buf, resourceMap)
	if err != nil {
		log.Panicf("Error executing template:\n%v\nResources:\n%v\nError:\n%v\n", templateContents, resourceMap, err)
	}

	result := buf.String()

	return result
}

func pemPrivateKeyFromPkcs12(b64pkcs12 string) string {
	log.Print("Getting private key from PKCS12 data")
	p12, _ := base64.StdEncoding.DecodeString(b64pkcs12)

	// Get the PEM Blocks
	blocks, err := pkcs12.ToPEM(p12, "")
	if err != nil {
		panic(err)
	}

	// Append all PEM Blocks together
	var pemData []byte
	for _, b := range blocks {
		pemData = append(pemData, pem.EncodeToMemory(b)...)
	}

	// Use tls lib to construct tls certificate and key object from PEM data
	// The tls.X509KeyPair function is smart enough to parse combined cert and key pem data
	certAndKey, err := tls.X509KeyPair(pemData, pemData)
	if err != nil {
		panic(err)
	}

	// Get parsed private key as PKCS8 data
	privBytes, err := x509.MarshalPKCS8PrivateKey(certAndKey.PrivateKey)
	if err != nil {
		log.Panicf("Unable to marshal private key: %v", err)
	}

	// Encode just the private key back to PEM and return it
	var privPem bytes.Buffer
	if err := pem.Encode(&privPem, &pem.Block{Type: "PRIVATE KEY", Bytes: privBytes}); err != nil {
		log.Panicf("Failed to write data to key.pem: %s", err)
	}

	return privPem.String()
}

func pemPrivateKeyFromPem(pem string) string {
	log.Print("Getting private key from PEM data")
	return pem
}
