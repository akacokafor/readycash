package readycash

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type mockStore struct {
	sync.RWMutex
	data map[string]interface{}
	timers []*time.Timer
}

func NewMockStore() *mockStore {
	return &mockStore{
		data: make(map[string]interface{}),
		timers: []*time.Timer{},
	}
}

func (m *mockStore) SetStringFor(key, val string, exp time.Duration) error {
	m.Lock()
	defer m.Unlock()
	m.data[key] = val
	m.timers = append(m.timers, time.AfterFunc(exp, func() {
		m.Lock()
		defer m.Unlock()
		delete(m.data, key)
	}))
	return nil
}

func (m *mockStore) SetIntFor(key string, val int64, exp time.Duration) error {
	m.Lock()
	defer m.Unlock()
	m.data[key] = val
	m.timers = append(m.timers, time.AfterFunc(exp, func() {
		m.Lock()
		defer m.Unlock()
		delete(m.data, key)
	}))
	return nil
}

func (m *mockStore) GetString(key string) (string, error) {
	m.RLock()
	defer m.RUnlock()
	output, ok := m.data[key]
	if !ok {
		return "", fmt.Errorf("data not found")
	}

	return fmt.Sprintf("%v", output), nil
}

func (m *mockStore) GetInt(key string) (int64, error) {
	m.RLock()
	defer m.RUnlock()
	output, ok := m.data[key]
	if !ok {
		return 0, fmt.Errorf("data not found")
	}

	if intVal, ok := output.(int64); ok {
		return intVal, nil
	}

	return 0, fmt.Errorf("not a number")
}

func TestBalanceEnquiry(t *testing.T)  {

	balanceRes := BalanceEnquiryResponse{
		Income: 5000,
		Main:   1000,
	}

	mockStoreInstance := NewMockStore()
	loginResults := struct {
		username string
		password string
		sessionLength string
	}{
		username:      "",
		password:      "",
		sessionLength: "0",
	}

	testResults := struct {
		requestCounter int
		loginCalled bool
		balanceEnquiryCalled bool
	}{
		requestCounter:       0,
		loginCalled:          false,
		balanceEnquiryCalled: false,
	}

	testAccount := Account{
		UserName:      "sample",
		Password:      "password",
		Pin:           "1234",
		SessionLength: time.Second * 3600,
	}

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		testResults.requestCounter += 1

		if req.URL.String() == baseLoginUrl {
			req.ParseForm()
			testResults.loginCalled = true
			loginResults.username = req.Form.Get("userName")
			loginResults.password = req.Form.Get("password")
			loginResults.sessionLength = req.Form.Get("sessionLength")
			rw.Header().Add("content-type","application/json")
			rw.Header().Add("Authorization","Bearer Token")
			rw.Header().Add("X-SessionID","1234")
			rw.WriteHeader(http.StatusOK)
			rw.Write([]byte(`{"first_time": false}`))
			return
		}

		if req.URL.String() == baseBalanceUrl {
			testResults.balanceEnquiryCalled = true
			rw.Header().Add("content-type","application/json")
			rw.WriteHeader(http.StatusOK)
			rw.Write([]byte(fmt.Sprintf(`{"income": "%f","main": "%f"}`,balanceRes.Income,balanceRes.Main)))
			return
		}

		rw.WriteHeader(http.StatusBadRequest)
		rw.Write([]byte(`OK`))
	}))
	defer server.Close()


	apiClient, err := NewClient(&testAccount,server.URL,mockStoreInstance,server.Client())
	if err != nil {
		t.Fatalf("Did not expect client creation to fail: %v", err)
	}

	apiClient.SetLogLevel(Debug)

	resp, err := apiClient.BalanceEnquiry()
	if err != nil {
		t.Fatalf("Did not expect call to fail: %v", err)
	}

	if resp.Income != balanceRes.Income {
		t.Errorf("Expected Income to be %v, Got %v", balanceRes.Income, resp.Income)
	}

	assert.Equal(t,2,testResults.requestCounter)
	assert.True(t,testResults.loginCalled)
	assert.True(t,testResults.balanceEnquiryCalled)
	assert.Equal(t, testAccount.UserName, loginResults.username)
	assert.Equal(t, testAccount.Password, loginResults.password)
}

func TestGenerateUSSD(t *testing.T)  {

	userRef := "user-defined-ref"
	amount := float64(1000)
	bankCode := "044"
	merchantRef := "0000000000011715"

	sampleResponse := fmt.Sprintf( `{
		"merchantRef": "%s",
		"transactionRef": "0000000000001070108",
		"ussdString": "*901*000*1111#",
		"amount": %2.f,
		"responseCode": "09",
		"transactionDate": 1622307058834,
		"expiryDate": 1622307350001,
		"paymentRef": null,
		"completionDate": null,
		"payerPhone": null,
		"paymentBank": null,
		"paymentNetwork": null,
		"paymentBankCode": null,
		"thirdParty": null,
		"status": "AWAITING CUSTOMER"
	}`,merchantRef,amount)
	mockStoreInstance := NewMockStore()
	loginResults := struct {
		username string
		password string
		sessionLength string
	}{
		username:      "",
		password:      "",
		sessionLength: "0",
	}

	testResults := struct {
		requestCounter     int
		loginCalled        bool
		generateUssdCalled bool
	}{
		requestCounter:     0,
		loginCalled:        false,
		generateUssdCalled: false,
	}

	testAccount := Account{
		UserName:      "sample",
		Password:      "password",
		Pin:           "1234",
		SessionLength: time.Second * 3600,
	}

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		testResults.requestCounter += 1

		if req.URL.String() == baseLoginUrl {
			req.ParseForm()
			testResults.loginCalled = true
			loginResults.username = req.Form.Get("userName")
			loginResults.password = req.Form.Get("password")
			loginResults.sessionLength = req.Form.Get("sessionLength")
			rw.Header().Add("content-type","application/json")
			rw.Header().Add("Authorization","Bearer Token")
			rw.Header().Add("X-SessionID","1234")
			rw.WriteHeader(http.StatusOK)
			rw.Write([]byte(`{}`))
			return
		}

		if req.URL.String() == baseUssdTransaction {
			testResults.generateUssdCalled = true
			rw.Header().Add("content-type","application/json")
			rw.WriteHeader(http.StatusOK)
			rw.Write([]byte(sampleResponse))
			return
		}

		rw.WriteHeader(http.StatusBadRequest)
		rw.Write([]byte(`OK`))
	}))
	defer server.Close()


	apiClient, err := NewClient(&testAccount,server.URL,mockStoreInstance,server.Client())
	if err != nil {
		t.Fatalf("Did not expect client creation to fail: %v", err)
	}

	apiClient.SetLogLevel(Debug)

	resp, err := apiClient.GenerateUSSD(userRef,amount,bankCode)
	if err != nil {
		t.Fatalf("Did not expect call to fail: %v", err)
	}

	assert.Equal(t,2,testResults.requestCounter)
	assert.True(t,testResults.loginCalled)
	assert.True(t,testResults.generateUssdCalled)
	assert.Equal(t, testAccount.UserName, loginResults.username)
	assert.Equal(t, testAccount.Password, loginResults.password)
	assert.Equal(t,userRef,resp.UserDefinedReference)
	assert.Equal(t,merchantRef,resp.MerchantRef)
}

func TestFetchUSSD(t *testing.T)  {

	userRef := "user-defined-ref"
	amount := float64(1000)
	bankCode := "044"
	merchantRef := "0000000000011715"

	sampleResponse := fmt.Sprintf( `{
    "merchantRef": "%s",
    "transactionRef": "0000000000001111111",
    "paymentRef": "ACCESS|USSD|11111111111111111|1111",
    "ussdString": "*901*000*1111#",
    "amount": %2.f,
    "responseCode": "00",
    "transactionDate": 1622307059000,
    "expiryDate": 1622307350000,
    "completionDate": 1622307102000,
    "payerPhone": "0803***1111",
    "paymentBank": "ACCESS BANK PLC",
    "paymentNetwork": "MTN",
    "paymentBankCode": "%s",
    "status": "SUCCESSFUL",
    "thirdParty": null
}`,merchantRef,amount,bankCode)
	mockStoreInstance := NewMockStore()
	loginResults := struct {
		username string
		password string
		sessionLength string
	}{
		username:      "",
		password:      "",
		sessionLength: "0",
	}

	testResults := struct {
		requestCounter     int
		loginCalled        bool
		generateUssdCalled bool
	}{
		requestCounter:     0,
		loginCalled:        false,
		generateUssdCalled: false,
	}

	testAccount := Account{
		UserName:      "sample",
		Password:      "password",
		Pin:           "1234",
		SessionLength: time.Second * 3600,
	}

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		t.Logf("Url Received: %s\n",req.URL.String())
		testResults.requestCounter += 1

		if req.URL.String() == baseLoginUrl {
			req.ParseForm()
			testResults.loginCalled = true
			loginResults.username = req.Form.Get("userName")
			loginResults.password = req.Form.Get("password")
			loginResults.sessionLength = req.Form.Get("sessionLength")
			rw.Header().Add("content-type","application/json")
			rw.Header().Add("Authorization","Bearer Token")
			rw.Header().Add("X-SessionID","1234")
			rw.WriteHeader(http.StatusOK)
			rw.Write([]byte(`{}`))
			return
		}

		if req.URL.String() == fmt.Sprintf("%s?senderRef=%s",baseFetchUssdTransaction,userRef) {
			testResults.generateUssdCalled = true
			rw.Header().Add("content-type","application/json")
			rw.WriteHeader(http.StatusOK)
			rw.Write([]byte(sampleResponse))
			return
		}

		rw.WriteHeader(http.StatusBadRequest)
		rw.Write([]byte(`OK`))
	}))
	defer server.Close()


	apiClient, err := NewClient(&testAccount,server.URL,mockStoreInstance,server.Client())
	if err != nil {
		t.Fatalf("Did not expect client creation to fail: %v", err)
	}

	apiClient.SetLogLevel(Debug)

	resp, err := apiClient.FetchUSSDTransaction(userRef)
	if err != nil {
		t.Fatalf("Did not expect call to fail: %v", err)
	}

	assert.Equal(t,2,testResults.requestCounter)
	assert.True(t,testResults.loginCalled)
	assert.True(t,testResults.generateUssdCalled)
	assert.Equal(t, testAccount.UserName, loginResults.username)
	assert.Equal(t, testAccount.Password, loginResults.password)
	assert.Equal(t,userRef,resp.UserDefinedReference)
	assert.Equal(t,merchantRef,resp.MerchantRef)
	assert.Equal(t,"SUCCESSFUL",resp.Status)
	assert.Equal(t,bankCode,*resp.PaymentBankCode)
}

func TestFetchTransactions(t *testing.T)  {

	emptyOptions := FetchTransactionOption{}

	transTypeOptions := FetchTransactionOption{
		TranType:  stringAddr("200.21.0001"),
	}

	allOptions := FetchTransactionOption{
		TranType: stringAddr("200.21.0001"),
		After:     intAddr(20),
		StartDate: intAddr(1622290322000),
		EndDate:   intAddr(1622307120000),
	}

	sampleResponse := `[
    {
        "debit": false,
        "sender": null,
        "recipient": null,
        "terminal": null,
        "tranId": 111111111,
        "tranType": "200.21.0001",
        "description": "USSD Cashback",
        "shortDescription": "USSD Cashback Deposit",
        "longDescription": "Money deposited using terminal USSD0000111111 ",
        "date": 1622307120000,
        "amount": 992.00,
        "reciept": {
            "amount": 992,
            "date": 1622307120000,
            "reference": "11111"
        },
        "balance": 14324.68,
        "balance2": null,
        "logoId": null,
        "externalRef": null,
        "sessionId": null,
        "timestamp": null,
        "captureDate": null,
        "narration": "USSD/0000111111/0000000000011111"
    },
    {
        "debit": true,
        "sender": null,
        "recipient": null,
        "terminal": null,
        "tranId": 32101362,
        "tranType": "200.22.0000",
        "description": "Cash IN",
        "shortDescription": "Cash IN",
        "longDescription": "Cash IN for 0000111111",
        "date": 1622290322000,
        "amount": 4500.00,
        "reciept": {
            "amount": 4.5E+3,
            "date": 1622290322000,
            "reference": "628935",
            "recipient": "0000111111"
        },
        "balance": 13332.68,
        "balance2": 9145.00,
        "logoId": null,
        "externalRef": null,
        "sessionId": null,
        "timestamp": null,
        "captureDate": null,
        "narration": null
    }]`

	sampleResponse2 := `[
    {
        "debit": true,
        "sender": null,
        "recipient": null,
        "terminal": null,
        "tranId": 32101362,
        "tranType": "200.22.0000",
        "description": "Cash IN",
        "shortDescription": "Cash IN",
        "longDescription": "Cash IN for 0000111111",
        "date": 1622290322000,
        "amount": 4500.00,
        "reciept": {
            "amount": 4.5E+3,
            "date": 1622290322000,
            "reference": "628935",
            "recipient": "0000111111"
        },
        "balance": 13332.68,
        "balance2": 9145.00,
        "logoId": null,
        "externalRef": null,
        "sessionId": null,
        "timestamp": null,
        "captureDate": null,
        "narration": null
    }]`

	sampleResponse3 := `[]`

	mockStoreInstance := NewMockStore()
	loginResults := struct {
		username string
		password string
		sessionLength string
	}{
		username:      "",
		password:      "",
		sessionLength: "0",
	}

	testResults := struct {
		requestCounter     int
		loginCalled        bool
		generateUssdCalled bool
	}{
		requestCounter:     0,
		loginCalled:        false,
		generateUssdCalled: false,
	}

	testAccount := Account{
		UserName:      "sample",
		Password:      "password",
		Pin:           "1234",
		SessionLength: time.Second * 3600,
	}

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		t.Logf("Url Received: %s\n",req.URL.String())
		testResults.requestCounter += 1
		req.ParseForm()

		if req.URL.String() == baseLoginUrl {
			testResults.loginCalled = true
			loginResults.username = req.Form.Get("userName")
			loginResults.password = req.Form.Get("password")
			loginResults.sessionLength = req.Form.Get("sessionLength")
			rw.Header().Add("content-type","application/json")
			rw.Header().Add("Authorization","Bearer Token")
			rw.Header().Add("X-SessionID","1234")
			rw.WriteHeader(http.StatusOK)
			rw.Write([]byte(`{}`))
			return
		}

		if req.URL.String() == baseTransactionsUrl {
			testResults.generateUssdCalled = true
			rw.Header().Add("content-type","application/json")
			rw.WriteHeader(http.StatusOK)
			rw.Write([]byte(sampleResponse))
			return
		}

		if req.URL.String() == fmt.Sprintf("%s?trantype=%s",baseTransactionsUrl,*transTypeOptions.TranType) {
			testResults.generateUssdCalled = true
			rw.Header().Add("content-type","application/json")
			rw.WriteHeader(http.StatusOK)
			rw.Write([]byte(sampleResponse2))
			return
		}

		if  strings.Contains(req.URL.String(), "?") &&
			req.URL.String()[:strings.Index(req.URL.String(),"?")] == baseTransactionsUrl &&
			req.Form.Get("trantype") == *allOptions.TranType &&
			req.Form.Get("after") == fmt.Sprintf("%d",*allOptions.After) &&
			req.Form.Get("start_date") == fmt.Sprintf("%d",*allOptions.StartDate) &&
			req.Form.Get("end_date") == fmt.Sprintf("%d",*allOptions.EndDate) {
			testResults.generateUssdCalled = true
			rw.Header().Add("content-type","application/json")
			rw.WriteHeader(http.StatusOK)
			rw.Write([]byte(sampleResponse3))
			return
		}

		rw.WriteHeader(http.StatusBadRequest)
		rw.Write([]byte(`OK`))
	}))
	defer server.Close()


	apiClient, err := NewClient(&testAccount,server.URL,mockStoreInstance,server.Client())
	if err != nil {
		t.Fatalf("Did not expect client creation to fail: %v", err)
	}

	apiClient.SetLogLevel(Debug)

	resp, err := apiClient.FetchTransaction(nil)
	if err != nil {
		t.Fatalf("Did not expect call to fail: %v", err)
	}
	assert.Len(t, resp, 2)

	resp_, err := apiClient.FetchTransaction(&emptyOptions)
	if err != nil {
		t.Fatalf("Did not expect call to fail: %v", err)
	}
	assert.Len(t, resp_, 2)

	resp1, err := apiClient.FetchTransaction(&transTypeOptions)
	if err != nil {
		t.Fatalf("Did not expect call to fail: %v", err)
	}
	assert.Len(t, resp1, 1)

	resp2, err := apiClient.FetchTransaction(&allOptions)
	if err != nil {
		t.Fatalf("Did not expect call to fail: %v", err)
	}
	assert.Len(t, resp2, 0)
}

func stringAddr(f string) *string {
	s := f
	return &s
}
func intAddr(f int64) *int64 {
	s := f
	return &s
}

