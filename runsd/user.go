package main

import (
	"fmt"
	"os/user"
	"strconv"
)

func resolveUser(uidOrUser string) (uint32, error) {
	i, err := strconv.ParseUint(uidOrUser, 10, 32)
	if err == nil {
		_, err := user.LookupId(uidOrUser)
		if err != nil {
			return 0, fmt.Errorf("cannot resolve user %d: %w", i, err)
		}
		return uint32(i), nil
	}
	u, err := user.Lookup(uidOrUser)
	if err != nil {
		return 0, fmt.Errorf("cannot resolve user %q: %w", uidOrUser, err)
	}
	i, err = strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("failed to parse uid %s: %w", u.Uid, err)
	}
	return uint32(i), nil
}
