/*
Copyright SecureKey Technologies Inc. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package operation

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/hyperledger/aries-framework-go/pkg/crypto"
	"github.com/hyperledger/aries-framework-go/pkg/kms"
	"github.com/trustbloc/edge-core/pkg/log"
	"github.com/trustbloc/edge-core/pkg/storage"

	support "github.com/trustbloc/hub-kms/pkg/internal/common"
	"github.com/trustbloc/hub-kms/pkg/keystore"
	kmsservice "github.com/trustbloc/hub-kms/pkg/kms"
)

const (
	// http params
	keystoreIDQueryParam = "keystoreID"
	keyIDQueryParam      = "keyID"

	// API endpoints
	kmsBasePath       = "/kms"
	keystoresEndpoint = kmsBasePath + "/keystores"
	keystoreEndpoint  = keystoresEndpoint + "/{" + keystoreIDQueryParam + "}"
	keysEndpoint      = keystoreEndpoint + "/keys"
	keyEndpoint       = keysEndpoint + "/{" + keyIDQueryParam + "}"
	signEndpoint      = keyEndpoint + "/sign"
	verifyEndpoint    = keyEndpoint + "/verify"
	encryptEndpoint   = keyEndpoint + "/encrypt"
	decryptEndpoint   = keyEndpoint + "/decrypt"

	// error messages
	receivedBadRequest       = "Received bad request: %s"
	createKeystoreFailure    = "Failed to create a keystore: %s"
	createKMSProviderFailure = "Failed to create a kms provider: %s"
	createKeyFailure         = "Failed to create a key: %s"
	signMessageFailure       = "Failed to sign a message: %s"
	verifyMessageFailure     = "Failed to verify a message: %s"
	encryptMessageFailure    = "Failed to encrypt a message: %s"
	decryptMessageFailure    = "Failed to decrypt a message: %s"
)

var logger = log.New("hub-kms/ops")

// Operation defines handlers logic for Key Server.
type Operation struct {
	handlers []Handler
	provider Provider
	logger   log.Logger
}

// Handler defines an HTTP handler for the API endpoint.
type Handler interface {
	Path() string
	Method() string
	Handle() http.HandlerFunc
}

// Provider contains dependencies for Operation.
type Provider interface {
	StorageProvider() storage.Provider
	KMSCreator() KMSCreator
	Crypto() crypto.Crypto
}

// KMSCreatorContext provides a context to the KMSCreator method.
type KMSCreatorContext struct {
	KeystoreID string
	Passphrase string
}

// KMSCreator provides a method for creating a new key manager for the KMS service.
type KMSCreator func(ctx KMSCreatorContext) (kms.KeyManager, error)

// New returns a new Operation instance.
func New(provider Provider) *Operation {
	op := &Operation{
		provider: provider,
	}

	op.registerHandlers()

	return op
}

// GetRESTHandlers gets all API handlers available for the Key Server service.
func (o *Operation) GetRESTHandlers() []Handler {
	return o.handlers
}

func (o *Operation) registerHandlers() {
	o.handlers = []Handler{
		support.NewHTTPHandler(keystoresEndpoint, http.MethodPost, o.createKeystoreHandler),
		support.NewHTTPHandler(keysEndpoint, http.MethodPost, o.createKeyHandler),
		support.NewHTTPHandler(signEndpoint, http.MethodPost, o.signHandler),
		support.NewHTTPHandler(verifyEndpoint, http.MethodPost, o.verifyHandler),
		support.NewHTTPHandler(encryptEndpoint, http.MethodPost, o.encryptHandler),
		support.NewHTTPHandler(decryptEndpoint, http.MethodPost, o.decryptHandler),
	}
}

func (o *Operation) createKeystoreHandler(rw http.ResponseWriter, req *http.Request) {
	var request createKeystoreReq
	if ok := parseRequest(&request, rw, req); !ok {
		return
	}

	repo, err := keystore.NewRepository(o.provider.StorageProvider())
	if err != nil {
		writeErrorResponse(rw, http.StatusInternalServerError, fmt.Sprintf(createKeystoreFailure, err))
		return
	}

	// TODO: Pass keystore.Service as a dependency (https://github.com/trustbloc/hub-kms/issues/29)
	srv := keystore.NewService(repo)
	keystoreID, err := srv.Create(request.Controller)
	if err != nil {
		writeErrorResponse(rw, http.StatusInternalServerError, fmt.Sprintf(createKeystoreFailure, err))
		return
	}

	rw.Header().Set("Location", keystoreLocation(req.Host, keystoreID))
	rw.WriteHeader(http.StatusCreated)
}

type kmsProvider struct {
	keystore keystore.Repository
	kms      kms.KeyManager
	crypto   crypto.Crypto
}

func (k kmsProvider) Keystore() keystore.Repository {
	return k.keystore
}

func (k kmsProvider) KMS() kms.KeyManager {
	return k.kms
}

func (k kmsProvider) Crypto() crypto.Crypto {
	return k.crypto
}

func (o *Operation) createKeyHandler(rw http.ResponseWriter, req *http.Request) {
	var request createKeyReq
	if ok := parseRequest(&request, rw, req); !ok {
		return
	}

	keystoreID := mux.Vars(req)[keystoreIDQueryParam]

	kmsProvider := prepareKMSProvider(rw, o.provider, keystoreID, request.Passphrase)
	if kmsProvider == nil {
		return
	}

	srv := kmsservice.NewService(kmsProvider)
	keyID, err := srv.CreateKey(keystoreID, kms.KeyType(request.KeyType))
	if err != nil {
		writeErrorResponse(rw, http.StatusInternalServerError, fmt.Sprintf(createKeyFailure, err))
		return
	}

	rw.Header().Set("Location", keyLocation(req.Host, keystoreID, keyID))
	rw.WriteHeader(http.StatusCreated)
}

func (o *Operation) signHandler(rw http.ResponseWriter, req *http.Request) {
	var request signReq
	if ok := parseRequest(&request, rw, req); !ok {
		return
	}

	keystoreID := mux.Vars(req)[keystoreIDQueryParam]
	keyID := mux.Vars(req)[keyIDQueryParam]

	provider := prepareKMSProvider(rw, o.provider, keystoreID, request.Passphrase)
	if provider == nil {
		return
	}

	srv := kmsservice.NewService(provider)
	signature, err := srv.Sign(keystoreID, keyID, []byte(request.Message))
	if err != nil {
		writeErrorResponse(rw, http.StatusInternalServerError, fmt.Sprintf(signMessageFailure, err))
		return
	}

	writeResponse(rw, signResp{
		Signature: base64.URLEncoding.EncodeToString(signature),
	})
}

func (o *Operation) verifyHandler(rw http.ResponseWriter, req *http.Request) {
	var request verifyReq
	if ok := parseRequest(&request, rw, req); !ok {
		return
	}

	keystoreID := mux.Vars(req)[keystoreIDQueryParam]
	keyID := mux.Vars(req)[keyIDQueryParam]

	provider := prepareKMSProvider(rw, o.provider, keystoreID, request.Passphrase)
	if provider == nil {
		return
	}

	signature, err := base64.URLEncoding.DecodeString(request.Signature)
	if err != nil {
		writeErrorResponse(rw, http.StatusBadRequest, fmt.Sprintf(receivedBadRequest, err))
		return
	}

	srv := kmsservice.NewService(provider)
	err = srv.Verify(keystoreID, keyID, signature, []byte(request.Message))
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, kmsservice.ErrInvalidSignature) {
			status = http.StatusOK
		}

		writeErrorResponse(rw, status, fmt.Sprintf(verifyMessageFailure, err))
		return
	}

	rw.WriteHeader(http.StatusOK)
}

func (o *Operation) encryptHandler(rw http.ResponseWriter, req *http.Request) {
	var request encryptReq
	if ok := parseRequest(&request, rw, req); !ok {
		return
	}

	keystoreID := mux.Vars(req)[keystoreIDQueryParam]
	keyID := mux.Vars(req)[keyIDQueryParam]

	provider := prepareKMSProvider(rw, o.provider, keystoreID, request.Passphrase)
	if provider == nil {
		return
	}

	srv := kmsservice.NewService(provider)
	cipherText, nonce, err := srv.Encrypt(keystoreID, keyID, []byte(request.Message), []byte(request.AdditionalData))
	if err != nil {
		writeErrorResponse(rw, http.StatusInternalServerError, fmt.Sprintf(encryptMessageFailure, err))
		return
	}

	writeResponse(rw, encryptResp{
		CipherText: base64.URLEncoding.EncodeToString(cipherText),
		Nonce:      base64.URLEncoding.EncodeToString(nonce),
	})
}

func (o *Operation) decryptHandler(rw http.ResponseWriter, req *http.Request) {
	var request decryptReq
	if ok := parseRequest(&request, rw, req); !ok {
		return
	}

	keystoreID := mux.Vars(req)[keystoreIDQueryParam]
	keyID := mux.Vars(req)[keyIDQueryParam]

	provider := prepareKMSProvider(rw, o.provider, keystoreID, request.Passphrase)
	if provider == nil {
		return
	}

	cipherText, err := base64.URLEncoding.DecodeString(request.CipherText)
	if err != nil {
		writeErrorResponse(rw, http.StatusBadRequest, fmt.Sprintf(receivedBadRequest, err))
		return
	}

	nonce, err := base64.URLEncoding.DecodeString(request.Nonce)
	if err != nil {
		writeErrorResponse(rw, http.StatusBadRequest, fmt.Sprintf(receivedBadRequest, err))
		return
	}

	srv := kmsservice.NewService(provider)
	plainText, err := srv.Decrypt(keystoreID, keyID, cipherText, []byte(request.AdditionalData), nonce)
	if err != nil {
		writeErrorResponse(rw, http.StatusInternalServerError, fmt.Sprintf(decryptMessageFailure, err))
		return
	}

	writeResponse(rw, decryptResp{
		PlainText: string(plainText),
	})
}

func parseRequest(parsedReq interface{}, rw http.ResponseWriter, req *http.Request) bool {
	if err := json.NewDecoder(req.Body).Decode(&parsedReq); err != nil {
		writeErrorResponse(rw, http.StatusBadRequest, fmt.Sprintf(receivedBadRequest, err))
		return false
	}

	return true
}

func prepareKMSProvider(rw http.ResponseWriter, provider Provider, keystoreID, passphrase string) *kmsProvider {
	keystoreRepo, err := keystore.NewRepository(provider.StorageProvider())
	if err != nil {
		writeErrorResponse(rw, http.StatusInternalServerError, fmt.Sprintf(createKMSProviderFailure, err))
		return nil
	}

	keyManager, err := provider.KMSCreator()(KMSCreatorContext{
		KeystoreID: keystoreID,
		Passphrase: passphrase,
	})
	if err != nil {
		writeErrorResponse(rw, http.StatusInternalServerError, fmt.Sprintf(createKMSProviderFailure, err))
		return nil
	}

	return &kmsProvider{
		keystore: keystoreRepo,
		kms:      keyManager,
		crypto:   provider.Crypto(),
	}
}

type errorResponse struct {
	Message string `json:"errMessage,omitempty"`
}

func writeErrorResponse(rw http.ResponseWriter, status int, msg string) {
	rw.WriteHeader(status)

	err := json.NewEncoder(rw).Encode(errorResponse{
		Message: msg,
	})

	if err != nil {
		logger.Errorf("Unable to send an error message, %s", err)
	}
}

func writeResponse(rw io.Writer, v interface{}) {
	err := json.NewEncoder(rw).Encode(v)
	if err != nil {
		logger.Errorf("Unable to send a response, %s", err)
	}
}

func keystoreLocation(hostURL, keystoreID string) string {
	// {hostURL}/kms/keystores/{keystoreID}
	return fmt.Sprintf("%s%s", hostURL,
		strings.ReplaceAll(keystoreEndpoint, "{keystoreID}", keystoreID))
}

func keyLocation(hostURL, keystoreID, keyID string) string {
	// {hostURL}/kms/keystores/{keystoreID}/keys/{keyID}
	r := strings.NewReplacer(
		"{keystoreID}", keystoreID,
		"{keyID}", keyID)

	return fmt.Sprintf("%s%s", hostURL, r.Replace(keyEndpoint))
}
