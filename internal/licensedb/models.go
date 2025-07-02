package licensedb

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// ModelEndpointPairing is a pairing of an API endpoint and a model.
// A [LicenseEntry] may only access an API endpoint with a specific model if it is granted a role that includes this pairing.
type ModelEndpointPairing struct {
	APIEndpoint string  `gorm:"primaryKey;column:api_endpoint;type:varchar(255);not null;check:api_endpoint != ''"`
	ModelName   string  `gorm:"primaryKey;column:model_name;type:varchar(255);not null;check:model_name != ''"`
	BillingID   uint    `gorm:"column:billing_id;not null"`
	Billing     Billing `gorm:"foreignKey:BillingID"`
}

func (m ModelEndpointPairing) String() string {
	return fmt.Sprintf("%s:%s", m.ModelName, m.APIEndpoint)
}

// DeleteModelEndpointPairing deletes the given model endpoint pairing from the database.
func (l *LicenseDB) DeleteModelEndpointPairing(ctx context.Context, modelName, apiEndpoint string) error {
	result := l.db.WithContext(ctx).
		Where("model_name = ?", modelName).
		Where("api_endpoint = ?", apiEndpoint).
		Delete(&ModelEndpointPairing{}) //nolint:exhaustruct
	if result.Error != nil {
		return fmt.Errorf("deleting model endpoint pairing %s:%s: %w", modelName, apiEndpoint, result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("no model endpoint pairing %s:%s found", modelName, apiEndpoint)
	}
	return nil
}

// Billing information for a [ModelEndpointPairing].
// Contains Stripe meter event names to call for billing purposes.
type Billing struct {
	gorm.Model
	StripeEventPromptTokens       string `gorm:"type:varchar(255)"`
	StripeEventCachedPromptTokens string `gorm:"type:varchar(255)"`
	StripeEventCompletionTokens   string `gorm:"type:varchar(255)"`
	StripeEventFileSizeMB         string `gorm:"type:varchar(255)"`
}

// UpdateModelEndpointPairingBilling updates the billing information for a given model endpoint pairing.
func (l *LicenseDB) UpdateModelEndpointPairingBilling(ctx context.Context, pairing ModelEndpointPairing, billing Billing) (ModelEndpointPairing, error) {
	var existingPairing ModelEndpointPairing
	result := l.db.WithContext(ctx).Where("model_name = ? AND api_endpoint = ?", pairing.ModelName, pairing.APIEndpoint).First(&existingPairing)
	if result.Error != nil {
		return existingPairing, fmt.Errorf("retrieving model endpoint pairing %s:%s: %w", pairing.ModelName, pairing.APIEndpoint, result.Error)
	}

	existingPairing.Billing = billing
	existingPairing.BillingID = billing.ID

	if err := l.db.WithContext(ctx).Save(&existingPairing).Error; err != nil {
		return existingPairing, fmt.Errorf("updating billing information for model endpoint pairing %s:%s: %w", pairing.ModelName, pairing.APIEndpoint, err)
	}

	return existingPairing, nil
}

// InsertBilling inserts a new billing record into the database.
func (l *LicenseDB) InsertBilling(ctx context.Context, billing Billing) (Billing, error) {
	result := l.db.WithContext(ctx).Create(&billing)
	if result.Error != nil {
		return billing, fmt.Errorf("inserting billing information: %w", result.Error)
	}

	return billing, nil
}

// DeleteBilling deletes a billing record by its ID.
func (l *LicenseDB) DeleteBilling(ctx context.Context, billingID uint) error {
	result := l.db.WithContext(ctx).Delete(&Billing{}, billingID) //nolint:exhaustruct
	if result.Error != nil {
		return fmt.Errorf("deleting billing information with ID %d: %w", billingID, result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("no billing information found with ID %d", billingID)
	}

	return nil
}
