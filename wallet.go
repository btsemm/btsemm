package btsemm

// GetWalletBalance returns all currency balances in the account.
func (c *Client) GetWalletBalance() ([]WalletBalance, error) {
	var out []WalletBalance
	return out, c.doAuthRaw("GET", "/api/v3.2/user/wallet", nil, nil, &out)
}

// GetCurrencyBalance returns the balance for a specific currency.
// Returns nil if the currency is not found.
func (c *Client) GetCurrencyBalance(currency string) (*WalletBalance, error) {
	balances, err := c.GetWalletBalance()
	if err != nil {
		return nil, err
	}
	for _, b := range balances {
		if b.Currency == currency {
			return &b, nil
		}
	}
	return nil, nil
}
