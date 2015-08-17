package rtype

import (
	"errors"
	"fmt"
	"time"
)

var errUndefined = errors.New("field undefined")

// Most types have a meta field attached to them to contain other various bits of information about them. Exceptions to
// this are types like DateRange, where I can't think of any possible reason I'd want to attach metadata specifically
// to a date range instead of the thing it's describing the date range of.

type dateLayouts []string

func (d dateLayouts) Parse(value string) (t time.Time, layout string, err error) {
	for _, l := range d {
		t, err = time.Parse(l, value)
		if err == nil {
			return t, l, err
		}
	}
	return time.Time{}, "", err
}

var layouts dateLayouts = []string{
	"2006-01-02 15:04:05 MST",
	"2006-01-02 15:04:05",
	"2006-01-02 15:04 MST",
	"2006-01-02 15:04",
	"2006-01-02",
	"2006-01",
	"2006",
}

type Resume struct {
	Me         Me           `yaml:"me"`
	Profiles   Profiles     `yaml:"profiles"`
	Employment []Employment `yaml:"work,omitempty"`
	Education  []Education  `yaml:"education,omitempty"`

	Meta map[string]interface{} `yaml:",inline"`
}

type Me struct {
	Order  []string `yaml:"ordered,flow"`
	Chosen string   `yaml:"chosen"`
	Phone  string   `yaml:"phone"`
	Email  string   `yaml:"email"`

	Meta map[string]interface{} `yaml:",inline"`
}

type Profiles struct {
	Order   []string           `yaml:".order,flow"`
	Profile map[string]Profile `yaml:",inline"`
}

type Profile struct {
	URL   string `yaml:"url,omitempty"`
	Label string `yaml:"label,omitempty"`

	Meta map[string]interface{} `yaml:",inline"`
}

type Employment struct {
	Title       string    `yaml:"title"`
	When        DateRange `yaml:"when"`
	Where       Place     `yaml:"where"`
	Description string    `yaml:"desc,omitempty"`

	Meta map[string]interface{} `yaml:",inline"`
}

type Education struct {
	Where       Place     `yaml:"where"`
	When        DateRange `yaml:"when"`
	Received    string    `yaml:"received,omitempty"`
	Fields      []string  `yaml:"fields,omitempty"`
	Description string    `yaml:"desc,omitempty"`

	Meta map[string]interface{} `yaml:",inline"`
}

type Place struct {
	Name  string `yaml:"name,omitempty"`
	Place string `yaml:"place,omitempty"`

	Meta map[string]interface{} `yaml:",inline"`
}

type DateRange struct {
	From time.Time `yaml:"from"`
	To   time.Time `yaml:"to"`

	fromLayout, toLayout string
}

func NewDateRange(from, to string) (d DateRange, err error) {
	err = d.parseFromTo(from, to)
	return d, err
}

type yamlDateRange struct {
	From string `yaml:"from,omitempty"`
	To   string `yaml:"to,omitempty"`
}

func (d DateRange) MarshalYAML() (interface{}, error) {
	var whence yamlDateRange

	if !d.From.IsZero() {
		if len(d.fromLayout) == 0 {
			d.fromLayout = layouts[4]
		}
		whence.From = d.From.Format(d.fromLayout)
	}

	if !d.To.IsZero() {
		if len(d.toLayout) == 0 {
			d.toLayout = layouts[4]
		}
		whence.To = d.To.Format(d.toLayout)
	}

	if len(whence.From) == 0 && len(whence.To) == 0 {
		return nil, nil
	}

	return whence, nil
}

func (d *DateRange) parseFromTo(from, to string) error {
	var fromErr, toErr error

	if d == nil {
		return errors.New("DateRange is nil")
	}

	r := *d
	if len(from) > 0 {
		r.From, r.fromLayout, fromErr = layouts.Parse(from)
	} else {
		fromErr = errUndefined
	}

	if len(to) > 0 {
		r.To, r.toLayout, toErr = layouts.Parse(to)
	} else {
		toErr = errUndefined
	}
	*d = r

	if fromErr != nil && toErr != nil && !(fromErr == errUndefined && toErr == errUndefined) {
		return fmt.Errorf("Cannot parse either from: nor to: field -- errors:\nfrom: %s\nto:   %s", fromErr, toErr)
	}

	return nil
}

func (d *DateRange) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var whence yamlDateRange
	if err := unmarshal(&whence); err != nil {
		return err
	}

	return d.parseFromTo(whence.From, whence.To)
}
