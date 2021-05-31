package readycash

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	terminalIDRegex       = regexp.MustCompile(`(?mi)[Mm]oney\s+deposited\s+using\s+terminal\s+(.+)?\s*`)
	posTransactionIDRegex = regexp.MustCompile(`(?mi)AGENT\s+POS\s+CASHBACK.*\s+(.+)?\s*`)
)

type ErrorResponse struct {
	Status           int
	Code             int
	Message          string
	DeveloperMessage string
}

func NewErrorResponse(status int, code int, message string, developerMessage string) *ErrorResponse {
	return &ErrorResponse{Status: status, Code: code, Message: message, DeveloperMessage: developerMessage}
}

func NewServerErrorResponse(message string) *ErrorResponse {
	return NewErrorResponse(500, 500, message, "")
}

func (e *ErrorResponse) Error() string {
	return fmt.Sprintf("Code: %d, Message: %s, Status: %d", e.Code, e.Message, e.Status)
}

type BalanceEnquiryResponse struct {
	Income float64 `json:"income"`
	Main   float64 `json:"main"`
}

func NewBalanceResponse(data []byte) (*BalanceEnquiryResponse, error) {
	var balanceMap map[string]interface{}
	if err := json.Unmarshal(data, &balanceMap); err != nil {
		return nil, err
	}

	var r BalanceEnquiryResponse

	if incomeStr, ok := balanceMap["income"]; ok {
		incomeFloat, err := strconv.ParseFloat(incomeStr.(string), 64)
		if err != nil {
			return nil, err
		}
		r.Income = incomeFloat
	}

	if mainStr, ok := balanceMap["main"]; ok {
		mainFloat, err := strconv.ParseFloat(mainStr.(string), 64)
		if err != nil {
			return nil, err
		}
		r.Main = mainFloat
	}

	return &r, nil
}

//UssdTransactionResponse returned from generate ussd operation
type UssdTransactionResponse struct {
	UserDefinedReference string  `json:"userDefinedReference"`
	MerchantRef          string  `json:"merchantRef"`
	TransactionRef       string  `json:"transactionRef"`
	UssdString           string  `json:"ussdString"`
	Amount               int64   `json:"amount"`
	ResponseCode         string  `json:"responseCode"`
	TransactionDate      int64   `json:"transactionDate"`
	ExpiryDate           int64   `json:"expiryDate"`
	CompletionDate       int64   `json:"completionDate"`
	Status               string  `json:"status"`
	PaymentRef           *string `json:"paymentRef"`
	PayerPhone           *string `json:"payerPhone"`
	PaymentBank          *string `json:"paymentBank"`
	PaymentNetwork       *string `json:"paymentNetwork"`
	PaymentBankCode      *string `json:"paymentBankCode"`
}

func NewUssdTransactionResponse(data []byte) (*UssdTransactionResponse, error) {
	var r UssdTransactionResponse
	err := json.Unmarshal(data, &r)
	return &r, err
}

func (r *UssdTransactionResponse) Marshal() ([]byte, error) {
	return json.Marshal(r)
}

type Reciept struct {
	Amount            float64 `json:"amount"`
	Date              int64   `json:"date"`
	Reference         string  `json:"reference"`
	Recipient         string  `json:"recipient"`
	TranType          string  `json:"tranType,omitempty"`
	ExternalReference string  `json:"externalReference,omitempty"`
	Bank              string  `json:"bank,omitempty"`
	Account           string  `json:"account,omitempty"`
	Name              string  `json:"name,omitempty"`
	Narration         string  `json:"narration,omitempty"`
	FormattedDate     string  `json:"formatted_date,omitempty"`
}

type WalletTransaction struct {
	Debit            bool    `json:"debit"`
	TranID           int64   `json:"tranId"`
	TranType         string  `json:"tranType"`
	Description      string  `json:"description"`
	ShortDescription string  `json:"shortDescription"`
	Narration        string  `json:"narration"`
	LongDescription  string  `json:"longDescription"`
	Date             int64   `json:"date"`
	Amount           float64 `json:"amount"`
	Reciept          Reciept `json:"reciept"`
	Balance          float64 `json:"balance"`
	Balance2         float64 `json:"balance2"`
	LogoID           string  `json:"logoId"`
	PosTerminalID    string  `json:"pos_terminal_id"`
	PosTransactionID string  `json:"pos_transaction_id"`
	FormattedDate    string  `json:"formatted_date"`
}

func (w *WalletTransaction) detectPosTerminalAndTransactionID() {
	terminalIDResult := terminalIDRegex.FindStringSubmatch(strings.TrimSpace(w.LongDescription))
	if len(terminalIDResult) > 1 {
		w.PosTerminalID = terminalIDResult[1]
	}

	posTransactionIDResult := posTransactionIDRegex.FindStringSubmatch(strings.TrimSpace(w.Narration))
	if len(posTransactionIDResult) > 1 {
		w.PosTransactionID = posTransactionIDResult[1]
	}
}

func NewWalletTransactions(data []byte) ([]WalletTransaction, error) {
	var walletTransactions []WalletTransaction
	if err := json.Unmarshal(data, &walletTransactions); err != nil {
		return nil, err
	}

	var result []WalletTransaction
	for _, transaction := range walletTransactions {
		t := transaction
		t.detectPosTerminalAndTransactionID()
		t.FormattedDate = time.Unix(t.Date/1000, 0).Format(time.RFC3339)
		t.Reciept.FormattedDate = time.Unix(t.Reciept.Date/1000, 0).Format(time.RFC3339)
		result = append(result, t)
	}

	return result, nil
}
