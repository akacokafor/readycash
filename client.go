package readycash

import (
	"bytes"
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

var (
	ErrAccountCredentialsRequired = errors.New("account credentials is required")
	ErrLoginFailed = errors.New("could not login to account")
	ErrBankNotSupportedOnUSSD = errors.New("bank not supported on ussd")
	ErrEmptyResponse = NewServerErrorResponse("Invalid Response Body")
)

type LogLevel int

const (
	Debug LogLevel = iota + 1
	Info
	Warn
	Error
)

const (
	providerSchoolable                = "SCHOOLABLE"
	reversedTransactionType           = "420.00.010.0000"
	thirtyDays                        = 30 * 24 * 60
)

const (
	baseUssdTransaction               = "/rc/rest/agent/transact/ussd/cashout"
	baseFetchUssdTransaction          = "/rc/rest/agent/transact/ussd/cashout/check"
	baseLoginUrl                      = "/rc/rest/agent/login"
	baseBalanceUrl                    = "/rc/rest/agent/balance"
	baseTransactionsUrl               = "/rc/rest/agent/tranlist"
	baseNameEnquiry                   = "/rc/rest/agent/transact/nameenquiry"
	baseBankFundsTransferUrl          = "/rc/rest/agent/transact/transfer/bank"
	baseWalletFundsTransferUrl        = "/rc/rest/agent/transact/transfer/mobile"
	baseAirtimeUrl                    = "/rc/rest/agent/transact/airtime"
	virtualBankAccountUrl             = "/rc/rest/agent/virtual_account/create"
	virtualBankAccountTransactionsUrl = "/rc/rest/agent/virtual_account/check/"
	createAgentUrl                    = "/rc/rest/agent/add_agent"
	createUserUrl                     = "/rc/rest/agent/register_user"
	linkPrepaidCard                   = "/rc/rest/agent/linkcard"
	checkTransaction                  = "/rc/rest/agent/transact/checktran"
	resolvePendingTransaction         = "/rc/rest/agent/transact/pending/resolve"
	listPendingTransactions           = "/rc/rest/agent/transact/pending/list"
	listBanks                         = "/rc/rest/common/institutions"
)

type authCacheKey struct {
	authorizationKey string
	sessionIDKey string
	expirationKey string
	encodedPinKey string
}

type authParams struct {
	authorization string
	sessionID string
	expiration time.Time
	encodedPin string
}

func (p *authParams) hasExpired() bool {
	if p.expiration.IsZero() {
		return true
	}

	if p.sessionID == "" || p.authorization == "" || p.encodedPin == "" {
		return true
	}

	return time.Now().After(p.expiration)
}

func (p *authParams) setPin(pin string, key string) error {
	hexStr, err := DesEncrypt([]byte(pin), []byte(key))
	p.encodedPin = hexStr
	return err
}

func (p *authParams) reset() {
	p.authorization = ""
	p.encodedPin = ""
	p.sessionID = ""
	p.expiration = time.Time{}
}

type FetchTransactionOption struct {
	TranType *string `json:"tran_type"`
	After *int64 `json:"after"`
	StartDate *int64 `json:"start_date"`
	EndDate *int64 `json:"end_date"`
}

func (o FetchTransactionOption) ToMap() map[string]string {
	result := make(map[string]string)

	if o.TranType != nil {
		result["trantype"] = *o.TranType
	}

	if o.After != nil {
		result["after"] = fmt.Sprintf("%d", *o.After)
	}

	if o.StartDate != nil {
		result["start_date"] = fmt.Sprintf("%d", *o.StartDate)
	}

	if o.EndDate != nil {
		result["end_date"] = fmt.Sprintf("%d", *o.EndDate)
	}

	return result
}

type Storage interface {
	SetStringFor(key, val string, exp time.Duration) error
	SetIntFor(key string, val int64, exp time.Duration) error
	GetString(key string) (string, error)
	GetInt(key string) (int64, error)
}

type Account struct {
	UserName string
	Password string
	Pin      string
	SessionLength time.Duration
}

type Client struct {
	account        *Account
	baseURL        string
	httpClient     *http.Client
	storage        Storage
	access         authParams
	logger         *logrus.Logger
}

func NewClient(
	account *Account,
	baseUrl string,
	storage Storage,
	httpClient *http.Client,
) (*Client, error) {

	if account == nil ||  account.Pin == "" || account.UserName == "" || account.Password == "" {
		return nil, ErrAccountCredentialsRequired
	}

	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	loggerInstance := logrus.New()
	loggerInstance.Level = logrus.ErrorLevel

	return &Client{
		storage: storage,
		account:    account,
		baseURL:    baseUrl,
		httpClient: httpClient,
		logger: loggerInstance,
	}, nil
}

//SetLogLevel changes the logging level for client
func (r *Client) SetLogLevel(l LogLevel) {
	switch l {
	case Debug:
		r.logger.Level = logrus.DebugLevel
	case Info:
		r.logger.Level = logrus.InfoLevel
	case Warn:
		r.logger.Level = logrus.WarnLevel
	case Error:
		r.logger.Level = logrus.ErrorLevel
	}
}

//BalanceEnquiry returns the account balance of the current user
func (r *Client) BalanceEnquiry() (*BalanceEnquiryResponse, error) {
	if err := r.ensureUserIsAuthenticated(); err != nil {
		return nil, err
	}

	balanceURL :=  r.generateUrl(baseBalanceUrl)
	request, err := r.newGetRequest( balanceURL, nil)
	if err != nil {
		return nil, err
	}

	statusCode, data, err := r.doRequest(request)
	if err != nil {
		return nil, err
	}

	if statusCode == http.StatusForbidden {
		r.access.reset()
		return r.BalanceEnquiry()
	}

	if !r.successCode(statusCode) {
		return nil, r.toErrorResponse(data)
	}

	return NewBalanceResponse(data)
}

//GenerateUSSD creates a new ussd for making payment into the wallet
func (r *Client) GenerateUSSD(
	reference string,
	amount float64,
	bankCode string,
) (*UssdTransactionResponse, error) {
	payload := map[string]interface{}{
		"amount":   amount,
		"bankCode": bankCode,
		"ref":      reference,
	}

	reqLogger := r.getRequestLogger(map[string]interface{}{
		"method": "GenerateUSSD",
	},payload)


	if !IsBankSupportedOnUSSD(bankCode) {
		reqLogger.Error("bank code not supported")
		return nil, ErrBankNotSupportedOnUSSD
	}

	if err := r.ensureUserIsAuthenticated(); err != nil {
		return nil, err
	}


	payloadReader, err := r.fromMapToReader(payload)
	if err != nil {
		reqLogger.WithError(err).Error("could not convert req payload to reader")
		return nil, err
	}

	ussdGenerationUrl := r.generateUrl(baseUssdTransaction)
	reqLogger.WithField("url", ussdGenerationUrl).Debug("ussd request url")

	request, err := r.newPostRequest(ussdGenerationUrl, payloadReader)
	if err != nil {
		reqLogger.WithError(err).Error("encountered error creating post request to generate ussd")
		return nil, err
	}

	statusCode, data, err := r.doRequest(request)
	if err != nil {
		reqLogger.WithError(err).Error("encountered error doing post request to generate ussd")
		return nil, err
	}

	if data == nil {
		reqLogger.Error("did not return any response content")
		return nil, ErrEmptyResponse
	}

	reqLogger.WithField("response", string(data)).Debug("ussd generation response")

	if !r.successCode(statusCode) {
		reqLogger.WithField("status_code",statusCode).
			WithField("data",string(data)).
			Error("status code received is not success")
		return nil, r.toErrorResponse(data)
	}

	res, err := NewUssdTransactionResponse(data)
	if err != nil {
		reqLogger.WithField("status_code",statusCode).
			WithField("data",string(data)).
			Error("error regenerating ussd transaction model from response")
		return nil, err
	}

	if res != nil {
		res.UserDefinedReference = reference
	}
	return res, nil
}

//FetchUSSDTransaction retrieves a ussd transaction by the user defined ref
func (r *Client) FetchUSSDTransaction(
	reference string,
) (*UssdTransactionResponse, error) {
	reqLogger := r.getRequestLogger(map[string]interface{}{
		"method": "FetchUSSDTransaction",
		"reference": reference,
	})

	if err := r.ensureUserIsAuthenticated(); err != nil {
		return nil, err
	}
	fetchUssdTransactionUrl := r.generateUrl(baseFetchUssdTransaction, map[string]string{
		"senderRef": reference,
	})
	request, err := r.newGetRequest(fetchUssdTransactionUrl, nil)
	if err != nil {
		reqLogger.WithError(err).Error("could not create get request")
		return nil, err
	}
	statusCode, data, err := r.doRequest(request)
	if err != nil {
		reqLogger.WithError(err).Error("could not initiate get request")
		return nil, err
	}
	if data == nil {
		reqLogger.Error("empty response received")
		return nil, ErrEmptyResponse
	}

	reqLogger.WithField("response", string(data)).Debug("ussd transaction fetch response")

	if !r.successCode(statusCode) {
		reqLogger.WithField("status_code", statusCode).
			WithField("data", string(data)).
			Error("status code received is not success")
		return nil, r.toErrorResponse(data)
	}

	res, err := NewUssdTransactionResponse(data)
	if err != nil {
		reqLogger.WithField("status_code", statusCode).
			WithField("data", string(data)).
			Error("error regenerating ussd transaction model from response")
		return nil, err
	}

	if res != nil {
		res.UserDefinedReference = reference
	}
	return res, nil
}

//FetchTransaction retrieves all transactions for the current user
func (r *Client) FetchTransaction(options *FetchTransactionOption) ([]WalletTransaction, error) {
	reqLogger := r.getRequestLogger(map[string]interface{}{
		"method": "FetchTransaction",
		"options": options,
	})

	if err := r.ensureUserIsAuthenticated(); err != nil {
		reqLogger.WithError(err).Error("could not ensure user is authenticated")
		return nil, err
	}

	queryParams := make(map[string]string)
	if options != nil {
		queryParams = options.ToMap()
	}
	transactionsUrl := r.generateUrl(baseTransactionsUrl,queryParams)
	request, err := r.newGetRequest(transactionsUrl, nil)
	if err != nil {
		reqLogger.WithError(err).Error("unable to create get request")
		return nil, err
	}

	statusCode,data, err := r.doRequest(request)
	if err != nil {
		reqLogger.WithError(err).Error("could not initiated get request")
		return nil, err
	}

	if data == nil {
		return nil, ErrEmptyResponse
	}

	if statusCode == http.StatusForbidden {
		r.access.reset()
		return r.FetchTransaction(options)
	}

	if !r.successCode(statusCode) {
		reqLogger.WithField("data", string(data)).WithField("status_code",statusCode).Error("status code not success")
		return nil, r.toErrorResponse(data)
	}

	return NewWalletTransactions(data)
}

func (r *Client) getRequestLogger(params ...map[string]interface{}) *logrus.Entry {
	vlog := r.logger.WithField("x-log-correlation-id", uuid.NewString())
	for _, p := range params {
		vlog = vlog.WithFields(p)
	}
	return vlog
}

func (r *Client) login() error {

	authCacheKey := r.makeAuthCacheKeys()
	authorizationKeyValue, err := r.storage.GetString(authCacheKey.authorizationKey)
	if err == nil {
		if authorizationKeyValue != "" {
			r.access.authorization = authorizationKeyValue
		}
	}
	sessionIDKeyValue, err := r.storage.GetString(authCacheKey.sessionIDKey)
	if err == nil {
		if sessionIDKeyValue != "" {
			r.access.sessionID = sessionIDKeyValue
		}
	}

	authEncodedPinValue, err := r.storage.GetString(authCacheKey.encodedPinKey)
	if err == nil {
		if authEncodedPinValue != "" {
			r.access.encodedPin = authEncodedPinValue
		}
	}
	authExpirationKeyValue, err := r.storage.GetInt(authCacheKey.expirationKey)
	if err == nil {
		if authExpirationKeyValue > 0 {
			_sessionExpiresAt := time.Unix(authExpirationKeyValue, 0)
			r.access.expiration = _sessionExpiresAt
		}
	}

	if !r.access.hasExpired() {
		return nil
	}

	payload := url.Values{
		"userName":      {r.account.UserName},
		"password":      {r.account.Password},
		"sessionLength": {fmt.Sprintf("%d", int64(r.account.SessionLength.Seconds()))},
	}
	loginURL := fmt.Sprintf("%s%s", r.baseURL, baseLoginUrl)
	res, err := r.httpClient.PostForm(loginURL, payload)
	if err != nil {
		return err
	}

	if res.Body != nil {
		defer r.tryCloseBody(res.Body)
	}

	bodyString, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return err
	}

	if !r.successCode(res.StatusCode) {
		return fmt.Errorf("%s %w", string(bodyString), ErrLoginFailed)
	}

	r.access.authorization = res.Header.Get("Authorization")
	r.access.sessionID = res.Header.Get("X-SessionID")
	if err := r.access.setPin(r.account.Pin, r.access.sessionID); err != nil {
		return err
	}

	r.access.expiration = time.Now().Add(r.account.SessionLength)

	if err := r.storage.SetStringFor(authCacheKey.authorizationKey, r.access.authorization, r.account.SessionLength); err != nil {
		return err
	}
	if err := r.storage.SetStringFor(authCacheKey.sessionIDKey, r.access.sessionID, r.account.SessionLength); err != nil {
		return err
	}
	if err := r.storage.SetStringFor(authCacheKey.encodedPinKey, r.access.encodedPin, r.account.SessionLength); err != nil {
		return err
	}
	if err := r.storage.SetIntFor(authCacheKey.expirationKey, r.access.expiration.Unix(), r.account.SessionLength); err != nil {
		return err
	}
	return nil
}

func (r *Client) successCode(statusCode int) bool {
	return statusCode == http.StatusOK || statusCode == http.StatusCreated ||statusCode == http.StatusAccepted
}

func (r *Client) makeAuthCacheKeys() authCacheKey {
	baseCacheKey := fmt.Sprintf("%x", md5.New().Sum([]byte(
		fmt.Sprintf("%s/%s/%s", r.baseURL, baseLoginUrl, r.account.UserName),
	)))
	authorizationKeyName := fmt.Sprintf("%s-auth-token", baseCacheKey)
	sessionIDKeyName := fmt.Sprintf("%s-session-id", baseCacheKey)
	authExpirationKeyName := fmt.Sprintf("%s-auth-expiration", baseCacheKey)
	authEncodedPinKeyName := fmt.Sprintf("%s-auth-encoded-pin", baseCacheKey)

	return authCacheKey{
		authorizationKeyName,
		sessionIDKeyName,
		authExpirationKeyName,
		authEncodedPinKeyName,
	}
}

func (r *Client) ensureUserIsAuthenticated() error {
	if r.hasSessionExpired() {
		if err := r.login(); err != nil {
			return err
		}
	}
	return nil
}

func (r *Client) hasSessionExpired() bool {
	return r.access.hasExpired()
}

func (r *Client) newGetRequest(url string, body io.Reader) (*http.Request, error) {
	return r.newRequest("GET",url,body)
}

func (r *Client) newPostRequest(url string, body io.Reader) (*http.Request, error) {
	return r.newRequest("POST",url,body)
}

func (r *Client) newRequest(method, url string, body io.Reader) (*http.Request, error) {
	var contents string
	if body != nil {
		buff := bytes.NewBufferString("")
		_, _ = io.Copy(buff, body)
	}
	r.logger.WithField("url", url).
		WithField("method",method).
		WithField("body",string(contents)).
		Debug("new request information")

	req, err := http.NewRequest(method, url,body)
	if err != nil {
		return nil, err
	}
	r.appendAuthHeaders(req)
	return req, nil
}

func (r *Client) appendAuthHeaders(request *http.Request) {
	request.Header.Add("Authorization", r.access.authorization)
	request.Header.Add("X-SessionID", r.access.sessionID)
	request.Header.Add("Content-Type", "application/json")
}

func (r *Client) tryCloseBody(body io.ReadCloser) {
	if body != nil {
		if err := body.Close(); err != nil {
			r.logger.WithError(err).Error("failed to close response body successfully")
		}
	}
}

func (r *Client) fromMapToReader(payload map[string]interface{}) (io.Reader, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		r.logger.WithError(err).Error("failed to encode ussd payload")
		return nil, err
	}

	r.logger.WithField("payload", string(payloadBytes)).Debug("ussd request payload")

	return bytes.NewReader(payloadBytes), nil
}

func (r *Client) doRequest(req *http.Request) (statusCode int, data []byte, err  error) {
	res, err := r.httpClient.Do(req)
	if err != nil {
		r.logger.WithError(err).Error("encountered error doing post request to generate ussd")
		return 0, nil, err
	}
	if res.Body != nil {
		defer r.tryCloseBody(res.Body)
	}
	if res.Body == nil {
		return res.StatusCode, nil, nil
	}
	data, err =  ioutil.ReadAll(res.Body)
	return res.StatusCode,data, err
}

func (r *Client) toErrorResponse(data []byte) error {
	var e ErrorResponse
	err := json.Unmarshal(data, &e)
	if err != nil {
		return err
	}
	return &e
}

func (r *Client) generateUrl(path string, queryParams ...map[string]string) string {
	reqUri, _ := url.Parse(fmt.Sprintf("%s%s", r.baseURL, path))
	queryVals, _ := url.ParseQuery(reqUri.RawQuery)
	if len(queryParams) > 0 {
		for _, queryParam := range queryParams {
			for k, v := range queryParam {
				queryVals.Add(k,v)
			}
		}
	}
	reqUri.RawQuery = queryVals.Encode()
	return reqUri.String()
}
