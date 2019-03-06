package main

import (
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/joho/godotenv"
)

func main() {
	//loadEnvironmentalVariables()

	//log to file as well as stdout
	f, err := os.OpenFile("output.txt", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("error opening file: %v", err)
	}
	defer f.Close()
	mw := io.MultiWriter(os.Stdout, f)
	log.SetOutput(mw)

	//set up telegram info
	bot, err := tgbotapi.NewBotAPI(os.Getenv("TELEGRAM_TOKEN"))
	errCheck(err, "Failed to start telegram bot")
	log.Printf("Authorized on account %s", bot.Self.UserName)
	chatID, err := strconv.ParseInt(os.Getenv("CHAT_ID"), 10, 64)
	errCheck(err, "Failed to fetch chat ID")

	client := &http.Client{}

	//for heroku
	go func() {
		http.ListenAndServe(":"+os.Getenv("PORT"),
			http.HandlerFunc(http.NotFound))
	}()
	for {
		//fetching cookies
		log.Println("Fetching cookies")
		aspxanon, sessionID := fetchCookies()

		//logging in
		log.Println("Logging in")
		loginForm := url.Values{}
		loginForm.Add("txtNRIC", os.Getenv("NRIC"))
		loginForm.Add("txtPassword", os.Getenv("PASSWORD"))
		loginForm.Add("btnLogin", " ")
		req, err := http.NewRequest("POST", "http://www.bbdc.sg/bbdc/bbdc_web/header2.asp",
			strings.NewReader(loginForm.Encode()))
		errCheck(err, "Error creating log in request")
		req.AddCookie(aspxanon)
		req.AddCookie(sessionID)
		req.AddCookie(&http.Cookie{Name: "language", Value: "en-US"})
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		_, err = client.Do(req)
		errCheck(err, "Error logging in")

		//fetching the booking page
		log.Println("Fetching booking page")
		req, err = http.NewRequest("POST", "http://www.bbdc.sg/bbdc/b-3c-pLessonBooking1.asp",
			strings.NewReader(bookingForm().Encode()))
		req.AddCookie(aspxanon)
		req.AddCookie(sessionID)
		req.AddCookie(&http.Cookie{Name: "language", Value: "en-US"})
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		errCheck(err, "Error creating get bookings request")
		resp, err := client.Do(req)
		errCheck(err, "Error fetching booking slots")
		body, _ := ioutil.ReadAll(resp.Body)
		//ioutil.WriteFile("booking.txt", body, 0644)

		//parse booking page to get booking dates
		//The data is hidden away in the following function call in the HTML page
		//fetched:
		//doTooltipV(event,0, "03/05/2019 (Fri)","3","11:30","13:10","BBDC");
		log.Println("Parsing booking page")
		foundSlot := false
		substrs := strings.Split(string(body), "doTooltipV(")[1:]
		for _, substr := range substrs {
			bookingData := strings.Split(substr, ",")[0:6]
			day := bookingData[2]
			monthInt := day[5:7]

			sessionNum := bookingData[3]
			if (strings.Contains(day, "Sat") || strings.Contains(day, "Sun")) && (monthInt == "02" || monthInt == "03" || monthInt == "04" || monthInt == "05") {
				alert("Slot available on "+day+" from "+bookingData[4]+" to "+bookingData[5],
					bot, chatID)
				foundSlot = true
			} else if (monthInt == "02" || monthInt == "03" || monthInt == "04") && (sessionNum == "\"7\"" || sessionNum == "\"8\"") {
				alert("Slot available on "+day+" from "+bookingData[4]+" to "+bookingData[5],
					bot, chatID)
				foundSlot = true
			}

			if foundSlot {
				//Check if the slot found is within 10 days to determine whether to auto book
				layout := "02/01/2006"
				dayProper, err := time.Parse(layout, strings.Split(strings.Split(day, "\"")[1], " ")[0])
				errCheck(err, "Error parsing date of slot")
				daysFromNow := int(dayProper.Sub(time.Now()).Hours()/24) + 1

				if daysFromNow <= 10 {
					//need to get slot ID for auto-book
					//strings.Split(substr, ",") returns- "BBDC"); SetMouseOverToggleColor("cell145_2") ' onmouseout='hideTip(); SetMouseOverToggleColor("cell145_2")'><input type="checkbox" id="145_2" name="slot" value="1893904" onclick="SetCountAndToggleColor('cell145_2'
					//splitting on value= and taking the second element returns- "1893904" onclick="SetCountAndToggleColor('cell145_2'
					//then split on " and take the second element to get 1893904
					slotID := strings.Split(strings.Split(strings.Split(substr, ",")[6], "value=")[1], "\"")[1]
					log.Println("Booking slot")
					req, err = http.NewRequest("POST", "http://www.bbdc.sg/bbdc/b-3c-pLessonBookingDetails.asp",
						strings.NewReader(paymentForm(slotID).Encode()))
					req.AddCookie(aspxanon)
					req.AddCookie(sessionID)
					req.AddCookie(&http.Cookie{Name: "language", Value: "en-US"})
					req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
					errCheck(err, "Error creating get bookings request")
					_, err = client.Do(req)
					errCheck(err, "Error creating booking slot")
					log.Println("Finished booking slot")

					alert("Auto-booked the slot as it was within 10 days of the current date", bot, chatID)
				}
			}
		}

		if foundSlot {
			alert("Finished getting slots", bot, chatID)
		} else {
			log.Println("No slots found")
		}
		r := rand.Intn(300) + 120
		time.Sleep(time.Duration(r) * time.Second)
	}

}

func alert(msg string, bot *tgbotapi.BotAPI, chatID int64) {
	telegramMsg := tgbotapi.NewMessage(chatID, msg)
	bot.Send(telegramMsg)
	log.Println("Sent message to " + strconv.FormatInt(chatID, 10) + ": " + msg)
}

func loadEnvironmentalVariables() {
	err := godotenv.Load()
	if err != nil {
		log.Print("Error loading environmental variables: ")
		log.Fatal(err.Error())
	}
}

func fetchCookies() (*http.Cookie, *http.Cookie) {
	resp, err := http.Get("http://www.bbdc.sg/bbweb/default.aspx")
	errCheck(err, "Error fetching cookies")
	aspxanon := resp.Cookies()[0]
	resp, err = http.Get("http://www.bbdc.sg/bbdc/bbdc_web/newheader.asp")
	errCheck(err, "Error fetching cookies (sessionID)")
	sessionID := resp.Cookies()[0]
	return aspxanon, sessionID
}

func paymentForm(slotID string) url.Values {
	form := url.Values{}
	form.Add("accId", os.Getenv("ACCOUNT_ID"))
	form.Add("slot", slotID)

	return form
}

func bookingForm() url.Values {
	bookingForm := url.Values{}
	bookingForm.Add("accId", os.Getenv("ACCOUNT_ID"))
	bookingForm.Add("Month", "Feb/2019")
	bookingForm.Add("Month", "Mar/2019")
	bookingForm.Add("Month", "Apr/2019")
	bookingForm.Add("Month", "May/2019")
	bookingForm.Add("Month", "Jun/2019")
	bookingForm.Add("Session", "1")
	bookingForm.Add("Session", "2")
	bookingForm.Add("Session", "3")
	bookingForm.Add("Session", "4")
	bookingForm.Add("Session", "5")
	bookingForm.Add("Session", "6")
	bookingForm.Add("Session", "7")
	bookingForm.Add("Session", "8")
	bookingForm.Add("allSes", "on")
	bookingForm.Add("Day", "2")
	bookingForm.Add("Day", "3")
	bookingForm.Add("Day", "4")
	bookingForm.Add("Day", "5")
	bookingForm.Add("Day", "6")
	bookingForm.Add("Day", "7")
	bookingForm.Add("Day", "1")
	bookingForm.Add("allDay", "")
	bookingForm.Add("defPLVenue", "1")
	bookingForm.Add("optVenue", "1")

	return bookingForm
}

func errCheck(err error, msg string) {
	if err != nil {
		log.Fatal(msg + ": " + err.Error())
	}
}
