package utility

import (
	"crypto/rc4"
	"github.com/jezard/jps-go/conf"
	"math"
)

func Dostuff() string {
	return "Hello"
}

var config = conf.Configuration()

//function to round values to defined number of places
func Round(val float64, roundOn float64, places int) (newVal float64) {
	var round float64
	pow := math.Pow(10, float64(places))
	digit := pow * val
	_, div := math.Modf(digit)
	if div >= roundOn {
		round = math.Ceil(digit)
	} else {
		round = math.Floor(digit)
	}
	newVal = round / pow
	return
}

func Decode(enc_user_id string) (string, error) {
	// if user not known..
	if enc_user_id == "unknown" {
		return "", nil
	}

	// decode our encoded user_id
	c, err := rc4.NewCipher([]byte(config.Cypher)) //our cipher
	var src []byte
	src = make([]byte, len(enc_user_id)) //not sure if this will work with foreign e.g. chinese or arabic email addresses
	copy(src, enc_user_id)

	c.XORKeyStream(src, src)
	tmp := make([]byte, len(src))
	for i := 0; i < len(src); i++ {
		tmp[i] = src[i]
	}

	user_id := string(tmp)
	return user_id, err

}
