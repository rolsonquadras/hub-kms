/*
Copyright SecureKey Technologies Inc. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package operation

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/tink/go/keyset"
	"github.com/google/tink/go/signature"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"

	"github.com/trustbloc/hub-kms/pkg/keystore"
	"github.com/trustbloc/hub-kms/pkg/kms"
)

const (
	createKeystoreReqFormat = `{
	  "controller": "%s"
	}`

	createKeyReqFormat = `{
	  "keyType": "%s"
	}`

	signReqFormat = `{
	  "message": "%s"
	}`

	verifyReqFormat = `{
	  "signature": "%s",
	  "message": "%s"
	}`

	encryptReqFormat = `{
	  "message": "%s",
	  "aad": "%s"
	}`

	decryptReqFormat = `{
	  "cipherText": "%s",
	  "aad": "%s",
	  "nonce": "%s"
	}`

	testKeyID      = "Fm4r2iwjYnswLRZKl38W"
	testKeystoreID = "bsi5ct08vcqmquc0fn5g"
	testController = "did:example:123456789"

	testKeyType    = "ED25519"
	testSignature  = "signature"
	testMessage    = "test message"
	testAAD        = "additional data"
	testCipherText = "cipher text"
	testNonce      = "nonce"
)

type failingResponseWriter struct {
	*httptest.ResponseRecorder
}

func (failingResponseWriter) Write(_ []byte) (int, error) {
	return 0, errors.New("write error")
}

func TestNew(t *testing.T) {
	srv := New(NewMockProvider())
	require.NotNil(t, srv)
	require.NotEmpty(t, srv.handlers)
}

func TestCreateKeystoreHandler(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		provider := NewMockProvider()
		op := New(provider)
		handler := getHandler(t, op, keystoresEndpoint, http.MethodPost)

		rr := httptest.NewRecorder()
		handler.Handle().ServeHTTP(rr, buildCreateKeystoreReq(t, testController))

		require.Equal(t, http.StatusCreated, rr.Code)
		require.NotEmpty(t, rr.Header().Get("Location"))
	})

	t.Run("Received bad request: EOF", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodPost, "", bytes.NewBuffer([]byte("")))
		require.NoError(t, err)

		op := New(NewMockProvider())
		handler := getHandler(t, op, keystoresEndpoint, http.MethodPost)

		rr := httptest.NewRecorder()
		handler.Handle().ServeHTTP(rr, req)

		require.Equal(t, http.StatusBadRequest, rr.Code)
		require.Contains(t, rr.Body.String(), fmt.Sprintf(receivedBadRequest, "EOF"))
	})

	t.Run("Failed to create storage for a keystore", func(t *testing.T) {
		provider := NewMockProvider()
		provider.MockStorage.ErrCreateStore = errors.New("create keystore error")

		op := New(provider)
		handler := getHandler(t, op, keystoresEndpoint, http.MethodPost)

		rr := httptest.NewRecorder()
		handler.Handle().ServeHTTP(rr, buildCreateKeystoreReq(t, testController))

		require.Equal(t, http.StatusInternalServerError, rr.Code)
		require.Contains(t, rr.Body.String(), strings.TrimSuffix(createKeystoreFailure, "%s"))
	})

	t.Run("Failed to create a keystore", func(t *testing.T) {
		provider := NewMockProvider()
		// TODO: Use keystore.Service mock to set an error (part of https://github.com/trustbloc/hub-kms/issues/29)
		provider.MockStorage.Store.ErrPut = errors.New("store put error")

		op := New(provider)
		handler := getHandler(t, op, keystoresEndpoint, http.MethodPost)

		rr := httptest.NewRecorder()
		handler.Handle().ServeHTTP(rr, buildCreateKeystoreReq(t, testController))

		require.Equal(t, http.StatusInternalServerError, rr.Code)
		require.Contains(t, rr.Body.String(), strings.TrimSuffix(createKeystoreFailure, "%s"))
	})
}

func TestCreateKeyHandler(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		provider := NewMockProvider()
		provider.MockKMS.CreateKeyID = testKeyID
		provider.MockStorage.Store.Store[testKeystoreID] = keystoreBytes(t)

		op := New(provider)
		handler := getHandler(t, op, keysEndpoint, http.MethodPost)

		rr := httptest.NewRecorder()
		handler.Handle().ServeHTTP(rr, buildCreateKeyReq(t, testKeyType))

		require.Equal(t, http.StatusCreated, rr.Code)
		require.NotEmpty(t, rr.Header().Get("Location"))
	})

	t.Run("Received bad request: EOF", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodPost, "", bytes.NewBuffer([]byte("")))
		require.NoError(t, err)

		op := New(NewMockProvider())
		handler := getHandler(t, op, keysEndpoint, http.MethodPost)

		rr := httptest.NewRecorder()
		handler.Handle().ServeHTTP(rr, req)

		require.Equal(t, http.StatusBadRequest, rr.Code)
		require.Contains(t, rr.Body.String(), fmt.Sprintf(receivedBadRequest, "EOF"))
	})

	t.Run("Failed to create a kms provider: open store", func(t *testing.T) {
		provider := NewMockProvider()
		provider.MockStorage.ErrOpenStoreHandle = errors.New("open store error")
		op := New(provider)
		handler := getHandler(t, op, keysEndpoint, http.MethodPost)

		rr := httptest.NewRecorder()
		handler.Handle().ServeHTTP(rr, buildCreateKeyReq(t, testKeyType))

		require.Equal(t, http.StatusInternalServerError, rr.Code)
		require.Contains(t, rr.Body.String(), strings.TrimSuffix(createKMSProviderFailure, "%s"))
	})

	t.Run("Failed to create a kms provider: kms creator error", func(t *testing.T) {
		provider := NewMockProvider()
		provider.KMSCreatorErr = errors.New("kms creator error")
		op := New(provider)
		handler := getHandler(t, op, keysEndpoint, http.MethodPost)

		rr := httptest.NewRecorder()
		handler.Handle().ServeHTTP(rr, buildCreateKeyReq(t, testKeyType))

		require.Equal(t, http.StatusInternalServerError, rr.Code)
		require.Contains(t, rr.Body.String(), strings.TrimSuffix(createKMSProviderFailure, "%s"))
	})

	t.Run("Failed to create a key: create key error", func(t *testing.T) {
		provider := NewMockProvider()
		provider.MockKMS.CreateKeyErr = errors.New("create key error")
		op := New(provider)
		handler := getHandler(t, op, keysEndpoint, http.MethodPost)

		rr := httptest.NewRecorder()
		handler.Handle().ServeHTTP(rr, buildCreateKeyReq(t, testKeyType))

		require.Equal(t, http.StatusInternalServerError, rr.Code)
		require.Contains(t, rr.Body.String(), strings.TrimSuffix(createKeyFailure, "%s"))
	})
}

func TestSignHandler(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		provider := NewMockProvider()
		provider.MockStorage.Store.Store[testKeystoreID] = keystoreBytes(t)
		provider.MockCrypto.SignValue = []byte("signature")

		op := New(provider)
		handler := getHandler(t, op, signEndpoint, http.MethodPost)

		rr := httptest.NewRecorder()
		handler.Handle().ServeHTTP(rr, buildSignReq(t, testMessage))

		require.Equal(t, http.StatusOK, rr.Code)
		require.Contains(t, rr.Body.String(), base64.URLEncoding.EncodeToString([]byte("signature")))
	})

	t.Run("Received bad request: EOF", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodPost, "", bytes.NewBuffer([]byte("")))
		require.NoError(t, err)

		op := New(NewMockProvider())
		handler := getHandler(t, op, signEndpoint, http.MethodPost)

		rr := httptest.NewRecorder()
		handler.Handle().ServeHTTP(rr, req)

		require.Equal(t, http.StatusBadRequest, rr.Code)
		require.Contains(t, rr.Body.String(), fmt.Sprintf(receivedBadRequest, "EOF"))
	})

	t.Run("Failed to create a kms provider: open store", func(t *testing.T) {
		provider := NewMockProvider()
		provider.MockStorage.ErrOpenStoreHandle = errors.New("open store error")
		op := New(provider)
		handler := getHandler(t, op, signEndpoint, http.MethodPost)

		rr := httptest.NewRecorder()
		handler.Handle().ServeHTTP(rr, buildSignReq(t, testMessage))

		require.Equal(t, http.StatusInternalServerError, rr.Code)
		require.Contains(t, rr.Body.String(), strings.TrimSuffix(createKMSProviderFailure, "%s"))
	})

	t.Run("Failed to create a kms provider: kms creator error", func(t *testing.T) {
		provider := NewMockProvider()
		provider.KMSCreatorErr = errors.New("kms creator error")
		op := New(provider)
		handler := getHandler(t, op, signEndpoint, http.MethodPost)

		rr := httptest.NewRecorder()
		handler.Handle().ServeHTTP(rr, buildSignReq(t, testMessage))

		require.Equal(t, http.StatusInternalServerError, rr.Code)
		require.Contains(t, rr.Body.String(), strings.TrimSuffix(createKMSProviderFailure, "%s"))
	})

	t.Run("Failed to sign a message: sign error", func(t *testing.T) {
		provider := NewMockProvider()
		provider.MockStorage.Store.Store[testKeystoreID] = keystoreBytes(t)
		provider.MockCrypto.SignErr = errors.New("sign error")
		op := New(provider)
		handler := getHandler(t, op, signEndpoint, http.MethodPost)

		rr := httptest.NewRecorder()
		handler.Handle().ServeHTTP(rr, buildSignReq(t, testMessage))

		require.Equal(t, http.StatusInternalServerError, rr.Code)
		require.Contains(t, rr.Body.String(), strings.TrimSuffix(signMessageFailure, "%s"))
	})
}

func TestVerifyHandler(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		kh, err := keyset.NewHandle(signature.ED25519KeyTemplate())
		require.NoError(t, err)

		provider := NewMockProvider()
		provider.MockStorage.Store.Store[testKeystoreID] = keystoreBytes(t)
		provider.MockKMS.GetKeyValue = kh

		op := New(provider)
		handler := getHandler(t, op, verifyEndpoint, http.MethodPost)

		sig := base64.URLEncoding.EncodeToString([]byte(testSignature))
		req := buildVerifyReq(t, sig)

		rr := httptest.NewRecorder()
		handler.Handle().ServeHTTP(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
	})

	t.Run("Success: invalid signature", func(t *testing.T) {
		kh, err := keyset.NewHandle(signature.ED25519KeyTemplate())
		require.NoError(t, err)

		provider := NewMockProvider()
		provider.MockStorage.Store.Store[testKeystoreID] = keystoreBytes(t)
		provider.MockKMS.GetKeyValue = kh
		provider.MockCrypto.VerifyErr = errors.New("verify msg: invalid signature")

		op := New(provider)
		handler := getHandler(t, op, verifyEndpoint, http.MethodPost)

		sig := base64.URLEncoding.EncodeToString([]byte(testSignature))
		req := buildVerifyReq(t, sig)

		rr := httptest.NewRecorder()
		handler.Handle().ServeHTTP(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		require.Contains(t, rr.Body.String(), kms.ErrInvalidSignature.Error())
	})

	t.Run("Received bad request: EOF", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodPost, "", bytes.NewBuffer([]byte("")))
		require.NoError(t, err)

		op := New(NewMockProvider())
		handler := getHandler(t, op, verifyEndpoint, http.MethodPost)

		rr := httptest.NewRecorder()
		handler.Handle().ServeHTTP(rr, req)

		require.Equal(t, http.StatusBadRequest, rr.Code)
		require.Contains(t, rr.Body.String(), fmt.Sprintf(receivedBadRequest, "EOF"))
	})

	t.Run("Received bad request: bad encoded signature", func(t *testing.T) {
		op := New(NewMockProvider())
		handler := getHandler(t, op, verifyEndpoint, http.MethodPost)

		rr := httptest.NewRecorder()
		handler.Handle().ServeHTTP(rr, buildVerifyReq(t, "!signature"))

		require.Equal(t, http.StatusBadRequest, rr.Code)
		require.Contains(t, rr.Body.String(), strings.TrimSuffix(receivedBadRequest, "%s"))
	})

	t.Run("Failed to create a kms provider: open store", func(t *testing.T) {
		provider := NewMockProvider()
		provider.MockStorage.ErrOpenStoreHandle = errors.New("open store error")
		op := New(provider)
		handler := getHandler(t, op, verifyEndpoint, http.MethodPost)

		sig := base64.URLEncoding.EncodeToString([]byte(testSignature))
		req := buildVerifyReq(t, sig)

		rr := httptest.NewRecorder()
		handler.Handle().ServeHTTP(rr, req)

		require.Equal(t, http.StatusInternalServerError, rr.Code)
		require.Contains(t, rr.Body.String(), strings.TrimSuffix(createKMSProviderFailure, "%s"))
	})

	t.Run("Failed to create a kms provider: kms creator error", func(t *testing.T) {
		provider := NewMockProvider()
		provider.KMSCreatorErr = errors.New("kms creator error")
		op := New(provider)
		handler := getHandler(t, op, verifyEndpoint, http.MethodPost)

		sig := base64.URLEncoding.EncodeToString([]byte(testSignature))
		req := buildVerifyReq(t, sig)

		rr := httptest.NewRecorder()
		handler.Handle().ServeHTTP(rr, req)

		require.Equal(t, http.StatusInternalServerError, rr.Code)
		require.Contains(t, rr.Body.String(), strings.TrimSuffix(createKMSProviderFailure, "%s"))
	})

	t.Run("Failed to verify a message: verify error", func(t *testing.T) {
		kh, err := keyset.NewHandle(signature.ED25519KeyTemplate())
		require.NoError(t, err)

		provider := NewMockProvider()
		provider.MockStorage.Store.Store[testKeystoreID] = keystoreBytes(t)
		provider.MockKMS.GetKeyValue = kh
		provider.MockCrypto.VerifyErr = errors.New("verify error")

		op := New(provider)
		handler := getHandler(t, op, verifyEndpoint, http.MethodPost)

		sig := base64.URLEncoding.EncodeToString([]byte(testSignature))
		req := buildVerifyReq(t, sig)

		rr := httptest.NewRecorder()
		handler.Handle().ServeHTTP(rr, req)

		require.Equal(t, http.StatusInternalServerError, rr.Code)
		require.Contains(t, rr.Body.String(), strings.TrimSuffix(verifyMessageFailure, "%s"))
	})
}

func TestEncryptHandler(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		provider := NewMockProvider()
		provider.MockStorage.Store.Store[testKeystoreID] = keystoreBytes(t)
		provider.MockCrypto.EncryptValue = []byte("cipher text")

		op := New(provider)
		handler := getHandler(t, op, encryptEndpoint, http.MethodPost)

		rr := httptest.NewRecorder()
		handler.Handle().ServeHTTP(rr, buildEncryptReq(t, testMessage, testAAD))

		require.Equal(t, http.StatusOK, rr.Code)
		require.Contains(t, rr.Body.String(), base64.URLEncoding.EncodeToString([]byte("cipher text")))
	})

	t.Run("Received bad request: EOF", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodPost, "", bytes.NewBuffer([]byte("")))
		require.NoError(t, err)

		op := New(NewMockProvider())
		handler := getHandler(t, op, encryptEndpoint, http.MethodPost)

		rr := httptest.NewRecorder()
		handler.Handle().ServeHTTP(rr, req)

		require.Equal(t, http.StatusBadRequest, rr.Code)
		require.Contains(t, rr.Body.String(), fmt.Sprintf(receivedBadRequest, "EOF"))
	})

	t.Run("Failed to create a kms provider: open store", func(t *testing.T) {
		provider := NewMockProvider()
		provider.MockStorage.ErrOpenStoreHandle = errors.New("open store error")
		op := New(provider)
		handler := getHandler(t, op, encryptEndpoint, http.MethodPost)

		rr := httptest.NewRecorder()
		handler.Handle().ServeHTTP(rr, buildEncryptReq(t, testMessage, testAAD))

		require.Equal(t, http.StatusInternalServerError, rr.Code)
		require.Contains(t, rr.Body.String(), strings.TrimSuffix(createKMSProviderFailure, "%s"))
	})

	t.Run("Failed to create a kms provider: kms creator error", func(t *testing.T) {
		provider := NewMockProvider()
		provider.KMSCreatorErr = errors.New("kms creator error")
		op := New(provider)
		handler := getHandler(t, op, encryptEndpoint, http.MethodPost)

		rr := httptest.NewRecorder()
		handler.Handle().ServeHTTP(rr, buildEncryptReq(t, testMessage, testAAD))

		require.Equal(t, http.StatusInternalServerError, rr.Code)
		require.Contains(t, rr.Body.String(), strings.TrimSuffix(createKMSProviderFailure, "%s"))
	})

	t.Run("Failed to encrypt a message: encrypt error", func(t *testing.T) {
		provider := NewMockProvider()
		provider.MockStorage.Store.Store[testKeystoreID] = keystoreBytes(t)
		provider.MockCrypto.EncryptErr = errors.New("encrypt error")
		op := New(provider)
		handler := getHandler(t, op, encryptEndpoint, http.MethodPost)

		rr := httptest.NewRecorder()
		handler.Handle().ServeHTTP(rr, buildEncryptReq(t, testMessage, testAAD))

		require.Equal(t, http.StatusInternalServerError, rr.Code)
		require.Contains(t, rr.Body.String(), strings.TrimSuffix(encryptMessageFailure, "%s"))
	})
}

func TestDecryptHandler(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		provider := NewMockProvider()
		provider.MockStorage.Store.Store[testKeystoreID] = keystoreBytes(t)
		provider.MockCrypto.DecryptValue = []byte("plain text")

		op := New(provider)
		handler := getHandler(t, op, decryptEndpoint, http.MethodPost)

		cipherText := base64.URLEncoding.EncodeToString([]byte(testCipherText))
		nonce := base64.URLEncoding.EncodeToString([]byte(testNonce))
		req := buildDecryptReq(t, cipherText, nonce)

		rr := httptest.NewRecorder()
		handler.Handle().ServeHTTP(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		require.Contains(t, rr.Body.String(), "plain text")
	})

	t.Run("Received bad request: EOF", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodPost, "", bytes.NewBuffer([]byte("")))
		require.NoError(t, err)

		op := New(NewMockProvider())
		handler := getHandler(t, op, decryptEndpoint, http.MethodPost)

		rr := httptest.NewRecorder()
		handler.Handle().ServeHTTP(rr, req)

		require.Equal(t, http.StatusBadRequest, rr.Code)
		require.Contains(t, rr.Body.String(), fmt.Sprintf(receivedBadRequest, "EOF"))
	})

	t.Run("Received bad request: bad encoded cipher text", func(t *testing.T) {
		op := New(NewMockProvider())
		handler := getHandler(t, op, decryptEndpoint, http.MethodPost)

		nonce := base64.URLEncoding.EncodeToString([]byte(testNonce))
		req := buildDecryptReq(t, "!cipher", nonce)

		rr := httptest.NewRecorder()
		handler.Handle().ServeHTTP(rr, req)

		require.Equal(t, http.StatusBadRequest, rr.Code)
		require.Contains(t, rr.Body.String(), strings.TrimSuffix(receivedBadRequest, "%s"))
	})

	t.Run("Received bad request: bad encoded nonce", func(t *testing.T) {
		op := New(NewMockProvider())
		handler := getHandler(t, op, decryptEndpoint, http.MethodPost)

		cipherText := base64.URLEncoding.EncodeToString([]byte(testCipherText))
		req := buildDecryptReq(t, cipherText, "!nonce")

		rr := httptest.NewRecorder()
		handler.Handle().ServeHTTP(rr, req)

		require.Equal(t, http.StatusBadRequest, rr.Code)
		require.Contains(t, rr.Body.String(), strings.TrimSuffix(receivedBadRequest, "%s"))
	})

	t.Run("Failed to create a kms provider: open store", func(t *testing.T) {
		provider := NewMockProvider()
		provider.MockStorage.ErrOpenStoreHandle = errors.New("open store error")
		op := New(provider)
		handler := getHandler(t, op, decryptEndpoint, http.MethodPost)

		cipherText := base64.URLEncoding.EncodeToString([]byte(testCipherText))
		nonce := base64.URLEncoding.EncodeToString([]byte(testNonce))
		req := buildDecryptReq(t, cipherText, nonce)

		rr := httptest.NewRecorder()
		handler.Handle().ServeHTTP(rr, req)

		require.Equal(t, http.StatusInternalServerError, rr.Code)
		require.Contains(t, rr.Body.String(), strings.TrimSuffix(createKMSProviderFailure, "%s"))
	})

	t.Run("Failed to create a kms provider: kms creator error", func(t *testing.T) {
		provider := NewMockProvider()
		provider.KMSCreatorErr = errors.New("kms creator error")
		op := New(provider)
		handler := getHandler(t, op, decryptEndpoint, http.MethodPost)

		cipherText := base64.URLEncoding.EncodeToString([]byte(testCipherText))
		nonce := base64.URLEncoding.EncodeToString([]byte(testNonce))
		req := buildDecryptReq(t, cipherText, nonce)

		rr := httptest.NewRecorder()
		handler.Handle().ServeHTTP(rr, req)

		require.Equal(t, http.StatusInternalServerError, rr.Code)
		require.Contains(t, rr.Body.String(), strings.TrimSuffix(createKMSProviderFailure, "%s"))
	})

	t.Run("Failed to decrypt a message: decrypt error", func(t *testing.T) {
		provider := NewMockProvider()
		provider.MockStorage.Store.Store[testKeystoreID] = keystoreBytes(t)
		provider.MockCrypto.DecryptErr = errors.New("decrypt error")
		op := New(provider)
		handler := getHandler(t, op, decryptEndpoint, http.MethodPost)

		cipherText := base64.URLEncoding.EncodeToString([]byte(testCipherText))
		nonce := base64.URLEncoding.EncodeToString([]byte(testNonce))
		req := buildDecryptReq(t, cipherText, nonce)

		rr := httptest.NewRecorder()
		handler.Handle().ServeHTTP(rr, req)

		require.Equal(t, http.StatusInternalServerError, rr.Code)
		require.Contains(t, rr.Body.String(), strings.TrimSuffix(decryptMessageFailure, "%s"))
	})

	t.Run("Failed to write an error response", func(t *testing.T) {
		provider := NewMockProvider()
		provider.MockStorage.Store.Store[testKeystoreID] = keystoreBytes(t)
		provider.MockCrypto.DecryptErr = errors.New("decrypt error")
		op := New(provider)
		handler := getHandler(t, op, decryptEndpoint, http.MethodPost)

		cipherText := base64.URLEncoding.EncodeToString([]byte(testCipherText))
		nonce := base64.URLEncoding.EncodeToString([]byte(testNonce))
		req := buildDecryptReq(t, cipherText, nonce)

		rr := failingResponseWriter{httptest.NewRecorder()}
		handler.Handle().ServeHTTP(rr, req)

		require.Empty(t, rr.Body.String())
	})

	t.Run("Failed to write a response", func(t *testing.T) {
		provider := NewMockProvider()
		provider.MockStorage.Store.Store[testKeystoreID] = keystoreBytes(t)
		provider.MockCrypto.DecryptValue = []byte("plain text")

		op := New(provider)
		handler := getHandler(t, op, decryptEndpoint, http.MethodPost)

		cipherText := base64.URLEncoding.EncodeToString([]byte(testCipherText))
		nonce := base64.URLEncoding.EncodeToString([]byte(testNonce))
		req := buildDecryptReq(t, cipherText, nonce)

		rr := failingResponseWriter{httptest.NewRecorder()}
		handler.Handle().ServeHTTP(rr, req)

		require.Empty(t, rr.Body.String())
	})
}

func getHandler(t *testing.T, op *Operation, pathToLookup, methodToLookup string) Handler {
	return getHandlerWithError(t, op, pathToLookup, methodToLookup)
}

func getHandlerWithError(t *testing.T, op *Operation, pathToLookup, methodToLookup string) Handler {
	return handlerLookup(t, op, pathToLookup, methodToLookup)
}

func handlerLookup(t *testing.T, op *Operation, pathToLookup, methodToLookup string) Handler {
	t.Helper()

	handlers := op.GetRESTHandlers()
	require.NotEmpty(t, handlers)

	for _, h := range handlers {
		if h.Path() == pathToLookup && h.Method() == methodToLookup {
			return h
		}
	}

	require.Fail(t, "unable to find handler")

	return nil
}

func keystoreBytes(t *testing.T) []byte {
	t.Helper()

	testKeystore := keystore.Keystore{
		ID:         testKeystoreID,
		Controller: testController,
		KeyIDs:     []string{testKeyID},
	}

	bytes, err := json.Marshal(testKeystore)
	require.NoError(t, err)

	return bytes
}

func buildCreateKeystoreReq(t *testing.T, controller string) *http.Request {
	t.Helper()

	payload := fmt.Sprintf(createKeystoreReqFormat, controller)
	req, err := http.NewRequest(http.MethodPost, "", bytes.NewBuffer([]byte(payload)))
	require.NoError(t, err)

	return req
}

func buildCreateKeyReq(t *testing.T, keyType string) *http.Request {
	t.Helper()

	payload := fmt.Sprintf(createKeyReqFormat, keyType)
	req, err := http.NewRequest(http.MethodPost, "", bytes.NewBuffer([]byte(payload)))
	require.NoError(t, err)

	req = mux.SetURLVars(req, map[string]string{
		"keystoreID": testKeystoreID,
	})

	return req
}

func buildSignReq(t *testing.T, message string) *http.Request {
	t.Helper()

	payload := fmt.Sprintf(signReqFormat, message)
	req, err := http.NewRequest(http.MethodPost, "", bytes.NewBuffer([]byte(payload)))
	require.NoError(t, err)

	req = mux.SetURLVars(req, map[string]string{
		"keystoreID": testKeystoreID,
		"keyID":      testKeyID,
	})

	return req
}

func buildVerifyReq(t *testing.T, signature string) *http.Request {
	t.Helper()

	payload := fmt.Sprintf(verifyReqFormat, signature, testMessage)
	req, err := http.NewRequest(http.MethodPost, "", bytes.NewBuffer([]byte(payload)))
	require.NoError(t, err)

	req = mux.SetURLVars(req, map[string]string{
		"keystoreID": testKeystoreID,
		"keyID":      testKeyID,
	})

	return req
}

func buildEncryptReq(t *testing.T, message, aad string) *http.Request {
	t.Helper()

	payload := fmt.Sprintf(encryptReqFormat, message, aad)
	req, err := http.NewRequest(http.MethodPost, "", bytes.NewBuffer([]byte(payload)))
	require.NoError(t, err)

	req = mux.SetURLVars(req, map[string]string{
		"keystoreID": testKeystoreID,
		"keyID":      testKeyID,
	})

	return req
}

func buildDecryptReq(t *testing.T, cipherText, nonce string) *http.Request {
	t.Helper()

	payload := fmt.Sprintf(decryptReqFormat, cipherText, testAAD, nonce)
	req, err := http.NewRequest(http.MethodPost, "", bytes.NewBuffer([]byte(payload)))
	require.NoError(t, err)

	req = mux.SetURLVars(req, map[string]string{
		"keystoreID": testKeystoreID,
		"keyID":      testKeyID,
	})

	return req
}
