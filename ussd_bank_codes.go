package readycash

var (
	BanksSupportedOnUssd = map[string]string{
		"057": "",
		"058": "",
		"033": "",
		"039": "",
		"232": "",
		"215": "",
		"082": "",
		"070": "",
		"050": "",
		"035": "",
		"044": "",
		"011": "",
		"214": "",
	}
)


func IsBankSupportedOnUSSD(bankCode string) bool {
	for bankCode, _ := range BanksSupportedOnUssd {
		if bankCode == bankCode {
			return true
		}
	}
	return false
}
