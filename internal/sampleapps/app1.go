package sampleapps

import (
	"fmt"
	"net"
	"strings"

	"github.com/racingmars/go3270"
)

var app1Screen1 = go3270.Screen{
	{Row: 0, Col: 27, Intense: true, Content: "3270 Example Application"},
	{Row: 2, Col: 0, Content: "Welcome to the go3270 example application. Please enter your name."},
	{Row: 4, Col: 0, Content: "First Name  . . ."},
	{Row: 4, Col: 19, Name: "fname", Write: true, Highlighting: go3270.Underscore},
	{Row: 4, Col: 39, Autoskip: true},
	{Row: 5, Col: 0, Content: "Last Name . . . ."},
	{Row: 5, Col: 19, Name: "lname", Write: true, Highlighting: go3270.Underscore},
	{Row: 5, Col: 39, Autoskip: true},
	{Row: 6, Col: 0, Content: "Password  . . . ."},
	{Row: 6, Col: 19, Name: "password", Write: true, Hidden: true},
	{Row: 6, Col: 39},
	{Row: 8, Col: 0, Content: "Press"},
	{Row: 8, Col: 6, Intense: true, Content: "enter"},
	{Row: 8, Col: 12, Content: "to submit your name."},
	{Row: 10, Col: 0, Intense: true, Color: go3270.Red, Name: "errormsg"},
	{Row: 22, Col: 0, Content: "PF3 Exit"},
}

var app1Screen2 = go3270.Screen{
	{Row: 0, Col: 27, Intense: true, Content: "3270 Example Application"},
	{Row: 2, Col: 0, Content: "Thank you for submitting your name. Here's what I know:"},
	{Row: 4, Col: 0, Content: "Your first name is"},
	{Row: 4, Col: 19, Name: "fname"},
	{Row: 5, Col: 0, Content: "And your last name is"},
	{Row: 5, Col: 22, Name: "lname"},
	{Row: 6, Col: 0, Name: "passwordOutput"},
	{Row: 8, Col: 0, Content: "Press"},
	{Row: 8, Col: 6, Intense: true, Content: "enter"},
	{Row: 8, Col: 12, Content: "to enter your name again, or"},
	{Row: 8, Col: 41, Intense: true, Content: "PF3"},
	{Row: 8, Col: 45, Content: "to quit and disconnect."},
	{Row: 11, Col: 0, Color: go3270.Turquoise, Highlighting: go3270.ReverseVideo, Content: "Here is a field with extended attributes."},
	{Row: 11, Col: 42},
	{Row: 22, Col: 0, Content: "PF3 Exit"},
}

func handleApp1(conn net.Conn) {
	defer conn.Close()

	go3270.NegotiateTelnet(conn)

	fieldValues := make(map[string]string)

mainLoop:
	for {
	screen1Loop:
		for {
			fieldValues["password"] = ""
			response, err := go3270.ShowScreen(app1Screen1, fieldValues, 4, 20, conn)
			if err != nil {
				return
			}

			if response.AID == go3270.AIDPF3 {
				break mainLoop
			}
			if response.AID != go3270.AIDEnter {
				continue screen1Loop
			}

			fieldValues = response.Values
			if strings.TrimSpace(fieldValues["fname"]) == "" &&
				strings.TrimSpace(fieldValues["lname"]) == "" {
				fieldValues["errormsg"] = "First and Last Name fields are required."
				continue screen1Loop
			}
			if strings.TrimSpace(fieldValues["fname"]) == "" {
				fieldValues["errormsg"] = "First Name field is required."
				continue screen1Loop
			}
			if strings.TrimSpace(fieldValues["lname"]) == "" {
				fieldValues["errormsg"] = "Last Name field is required."
				continue screen1Loop
			}

			fieldValues["errormsg"] = ""
			break screen1Loop
		}

		passwordLength := len(strings.TrimSpace(fieldValues["password"]))
		passwordPlural := "s"
		if passwordLength == 1 {
			passwordPlural = ""
		}
		fieldValues["passwordOutput"] = fmt.Sprintf("Your password was %d character%s long",
			passwordLength, passwordPlural)

		response, err := go3270.ShowScreen(app1Screen2, fieldValues, 0, 0, conn)
		if err != nil {
			return
		}
		if response.AID == go3270.AIDPF3 {
			break
		}
	}
}
