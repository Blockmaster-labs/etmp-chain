package awskms

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"time"

	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/secrets"
	"github.com/0xPolygon/polygon-edge/secrets/local"
	"github.com/hashicorp/go-hclog"
)

// KmsSecretManager is a SecretsManager that
// stores secrets on a aws kms instance
type KmsSecretManager struct {
	// Logger object
	logger hclog.Logger

	// Token used for kms instance authentication
	token string

	// The Server URL of the kms instance
	serverURL string

	// The name of the current node, used for prefixing names of secrets
	name string

	// The base path to store the secrets in the  kms storage
	basePath string

	// The HTTP client used for interacting with the kms server
	client *http.Client

	// libp2p key use the local secrets manager
	localSM secrets.SecretsManager

	// init phase, cache the validator pubkey
	validatorPubkey string

	// init phase, cache the validator address
	validatorAddress string
}

// SecretsManagerFactory implements the factory method
func SecretsManagerFactory(
	config *secrets.SecretsManagerConfig,
	params *secrets.SecretsManagerParams,
) (secrets.SecretsManager, error) {
	// Set up the base object
	kmsManager := &KmsSecretManager{
		logger: params.Logger.Named(string(secrets.AwsKms)),
	}

	// Check if the token is present, current is the key name
	if config.Token == "" {
		return nil, errors.New("no token specified for kms secrets manager")
	}

	// Grab the token from the config
	kmsManager.token = config.Token

	// Check if the server URL is present
	if config.ServerURL == "" {
		return nil, errors.New("no server URL specified for kms secrets manager")
	}

	// Grab the server URL from the config
	kmsManager.serverURL = config.ServerURL

	// Check if the node name is present
	if config.Name == "" {
		return nil, errors.New("no node name specified for kms secrets manager")
	}

	// Grab the node name from the config
	kmsManager.name = config.Name

	// Set the base path to store the secrets in the KV-2 kms storage
	kmsManager.basePath = fmt.Sprintf("secret/data/%s", kmsManager.name)

	// Run the initial setup
	_ = kmsManager.Setup()

	// Init the local secrets manager
	var err error
	kmsManager.localSM, err = local.SecretsManagerFactory(
		nil, // Local secrets manager doesn't require a config
		params,
	)

	if err != nil {
		return nil, err
	}

	return kmsManager, nil
}

// Setup sets up the Hashicorp kms secrets manager
func (k *KmsSecretManager) Setup() error {
	tr := &http.Transport{
		MaxIdleConns:       10,
		IdleConnTimeout:    30 * time.Second,
		DisableCompression: true,
	}
	k.client = &http.Client{Transport: tr}

	return nil
}

// GetSecret gets the secret by name
func (k *KmsSecretManager) GetSecret(name string) ([]byte, error) {
	switch name {
	case secrets.ValidatorKey:
		return k.GetSecretFromKms(name)

	case secrets.NetworkKey:
		return k.localSM.GetSecret(name)

	default:
		return nil, errors.New("not support getsecret name")
	}
}

func (k *KmsSecretManager) GetSecretFromKms(name string) ([]byte, error) {
	// read from kms , by http post

	return nil, nil
}

// SetSecret sets the secret to a provided value
func (k *KmsSecretManager) SetSecret(name string, value []byte) error {
	switch name {
	case secrets.ValidatorKey:
		return errors.New("aws kms not support setsecret")

	case secrets.NetworkKey:
		return k.localSM.SetSecret(name, value)

	default:
		return errors.New("not support setsecret name")
	}
}

// HasSecret checks if the secret is present
func (k *KmsSecretManager) HasSecret(name string) bool {
	switch name {
	case secrets.ValidatorKey:
		return true //Todo: wait confirm the scecnaciro

	case secrets.NetworkKey:
		return k.localSM.HasSecret(name)

	default:
		return true
	}
}

// RemoveSecret removes the secret from storage
func (k *KmsSecretManager) RemoveSecret(name string) error {
	switch name {
	case secrets.ValidatorKey:
		return errors.New("aws kms not support RemoveSecret")

	case secrets.NetworkKey:
		return k.localSM.RemoveSecret(name)

	default:
		return errors.New("not support RemoveSecret name")
	}
}

// Sign data by key
func (k *KmsSecretManager) SignBySecret(key string, data []byte) ([]byte, error) {

	type SignRaw struct {
		KmsKeyId string `json:"kms_key_id"`
		Data     string `json:"data"`
	}

	type Req struct {
		Operation string  `json:"operation"`
		SignRaw   SignRaw `json:"sign_raw"`
	}
	req := &Req{
		Operation: "sign_raw",
		SignRaw: SignRaw{
			KmsKeyId: k.name,
			Data:     string(data),
		},
	}

	bs, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	fmt.Println("reqData: ", string(bs))

	resp, err := k.client.Post(k.serverURL, "application/json", bytes.NewBuffer(bs))
	if err != nil {
		fmt.Println("http post errr", err)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Println("Status code err", resp.StatusCode)
		return nil, errors.New("http status error")
	}

	type SignRawData struct {
		R string `json:"r"`
		S string `json:"s"`
		V int    `json:"v"`
	}

	type Resp struct {
		Code int         `json:"code"`
		Msg  string      `json:"msg"`
		Data SignRawData `json:"data"`
	}

	// var rspIns SignResp

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	fmt.Println("respData: ", string(respData))

	// var respMap map[string]interface{}
	var signResp Resp

	err = json.Unmarshal(respData, &signResp)
	if err != nil {
		return nil, err
	}

	if signResp.Code != 0 {
		return nil, fmt.Errorf("get info json data err %s ", signResp.Msg)
	}

	fmt.Println("map: ", signResp)

	// secretInfo := &secrets.SecretInfo{
	// 	Pubkey:  infoResp.Data.PubKey,
	// 	Address: infoResp.Data.Address,
	// }

	R, ok := (&big.Int{}).SetString(signResp.Data.R[2:], 16)
	if !ok {
		return nil, errors.New("R to big int error")
	}

	S, ok := (&big.Int{}).SetString(signResp.Data.R[2:], 16)
	if !ok {
		return nil, errors.New("S to big int error")
	}

	// v := []byte(signResp.Data.V)

	buf := new(bytes.Buffer)
	binary.Write(buf, binary.BigEndian, int32(signResp.Data.V))
	// binary.Write(buf, binary.LittleEndian, int32(signResp.Data.V))
	fmt.Println("bytes :", buf.Bytes())
	// // return

	fmt.Println(R, S, signResp.Data.V)

	// return crypto.EncodeSignature(R, S, buf.Bytes()[0])
	return crypto.EncodeSignature(R, S, buf.Bytes()[0])

	// return secretInfo, nil

}

func (k *KmsSecretManager) GetSecretInfo(name string) (*secrets.SecretInfo, error) {
	if name != secrets.ValidatorKey {
		return nil, errors.New("not support GetSecretInfo name")
	}

	type InfoReq struct {
		KmsKeyId string `json:"kms_key_id"`
	}

	type Req struct {
		Operation string  `json:"operation"`
		Info      InfoReq `json:"info"`
		//   string `json:"kms_key_id"`
	}
	req := &Req{
		Operation: "info",
		Info: InfoReq{
			KmsKeyId: k.name,
		},
	}

	bs, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	fmt.Println("reqData: ", string(bs))

	resp, err := k.client.Post(k.serverURL, "application/json", bytes.NewBuffer(bs))
	if err != nil {
		fmt.Println("http post errr", err)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Println("Status code err", resp.StatusCode)
		return nil, errors.New("http status error")
	}

	type InfoData struct {
		Address string `json:"address"`
		PubKey  string `json:"pub_key"`
	}

	type Resp struct {
		Code int      `json:"code"`
		Msg  string   `json:"msg"`
		Data InfoData `json:"data"`
	}

	// var rspIns SignResp

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	fmt.Println("respData: ", string(respData))

	// var respMap map[string]interface{}
	var infoResp Resp

	err = json.Unmarshal(respData, &infoResp)
	if err != nil {
		return nil, err
	}

	if infoResp.Code != 0 {
		return nil, fmt.Errorf("get info json data err %s ", infoResp.Msg)
	}

	fmt.Println("map: ", infoResp)

	secretInfo := &secrets.SecretInfo{
		Pubkey:  infoResp.Data.PubKey,
		Address: infoResp.Data.Address,
	}

	return secretInfo, nil

}

// get SecretsManagerType
func (k *KmsSecretManager) GetSecretsManagerType() secrets.SecretsManagerType {
	return secrets.AwsKms
}