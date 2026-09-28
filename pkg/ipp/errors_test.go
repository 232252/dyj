package ipp

import "errors"

var (
	errShortResponse = errors.New("ipp: response is shorter than its structure claims")
	errGarbage       = errors.New("ipp: attribute found outside any group")
)
