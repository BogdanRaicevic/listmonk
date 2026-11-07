package core

// Fork: complaint handling across the variants of an address.

import (
	"github.com/knadh/listmonk/models"
)

// applyComplaintToVariants applies a complaint's bounce action (blocklist or
// unsubscribe) to every subscriber whose address reaches the same mailbox as the
// complaining one, eg: john.doe+news@gmail.com and johndoe@gmail.com, once the
// complaints across those addresses reach the action's count. unstored is true
// when the current complaint wasn't recorded (the address isn't a subscriber),
// so that it's counted too. Other bounce types and actions are left alone.
func (c *Core) applyComplaintToVariants(b models.Bounce, action string, count int, unstored bool) error {
	if b.Type != models.BounceTypeComplaint || (action != "blocklist" && action != "unsubscribe") {
		return nil
	}
	if b.Email == "" && b.SubscriberUUID == "" {
		return nil
	}

	extra := 0
	if unstored {
		extra = 1
	}
	if _, err := c.q.ApplyComplaintToVariants.Exec(b.Email, b.SubscriberUUID, action, count, extra); err != nil {
		c.log.Printf("error applying complaint to the address's variants: %v", err)
		return err
	}
	return nil
}

// IsEmailSuppressed reports whether an address must not be sent transactional
// e-mail: one of its variants complained, or the address hard-bounced.
func (c *Core) IsEmailSuppressed(email string) (bool, error) {
	var out bool
	if err := c.q.IsEmailSuppressed.Get(&out, email); err != nil {
		c.log.Printf("error checking e-mail suppression: %v", err)
		return false, err
	}
	return out, nil
}
