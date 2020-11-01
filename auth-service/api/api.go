package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"strings"

	"github.com/dgrijalva/jwt-go"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/joho/godotenv"
	"github.com/sendgrid/sendgrid-go"
	"golang.org/x/crypto/bcrypt"
)

const (
	verifyTokenSize = 6
	resetTokenSize  = 6
)

// RegisterRoutes initializes the api endpoints and maps the requests to specific functions
func RegisterRoutes(router *mux.Router) error {
	router.HandleFunc("/api/auth/signup", signup).Methods(http.MethodPost, http.MethodOptions)
	router.HandleFunc("/api/auth/signin", signin).Methods(http.MethodPost, http.MethodOptions)
	router.HandleFunc("/api/auth/logout", logout).Methods(http.MethodPost, http.MethodOptions)
	router.HandleFunc("/api/auth/verify", verify).Methods(http.MethodPost, http.MethodOptions)
	router.HandleFunc("/api/auth/sendreset", sendReset).Methods(http.MethodPost, http.MethodOptions)
	router.HandleFunc("/api/auth/resetpw", resetPassword).Methods(http.MethodPost, http.MethodOptions)

	// Load sendgrid credentials
	err := godotenv.Load()
	if err != nil {
		return err
	}

	sendgridKey = os.Getenv("SENDGRID_KEY")
	sendgridClient = sendgrid.NewSendClient(sendgridKey)
	return nil
}

func signup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "http://localhost:3000")
	w.Header().Set("Access-Control-Allow-Headers", "content-type")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Credentials", "true")

	if (*r).Method == "OPTIONS" {
		return
	}

	//Obtain the credentials from the request body
	// YOUR CODE HERE
	credentials := Credentials{}

	jsonDecoder := json.NewDecoder(r.Body)

	err := jsonDecoder.Decode(&credentials)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	username := credentials.Username
	email := credentials.Email
	password := credentials.Password

	//Check if the username already exists
	var exists bool
	err = DB.QueryRow("SELECT exists(SELECT * FROM users WHERE username=?)", username).Scan(&exists)

	//Check for error
	if err != nil {
		http.Error(w, errors.New("error checking if username exists").Error(), http.StatusInternalServerError)
		log.Print(err.Error())
		return
	}

	//Check boolean returned from query
	if exists == true {
		http.Error(w, errors.New("this username is taken").Error(), http.StatusConflict)
		return
	}

	//Check if the email already exists
	err = DB.QueryRow("SELECT exists(SELECT * FROM users WHERE email=?)", email).Scan(&exists)

	//Check for error
	// YOUR CODE HERE
	if err != nil {
		http.Error(w, errors.New("error checking if email exists").Error(), http.StatusInternalServerError)
		log.Print(err.Error())
		return
	}

	//Check boolean returned from query
	// YOUR CODE HERE
	if exists == true {
		http.Error(w, errors.New("this email is taken").Error(), http.StatusConflict)
		return
	}

	//Hash the password using bcrypt and store the hashed password in a variable
	// YOUR CODE HERE
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)

	//Check for errors during hashing process
	// YOUR CODE HERE
	if err != nil {
		http.Error(w, "Server error, unable to create your account.", 500)
	}

	//Create a new user UUID, convert it to string, and store it within a variable
	// YOUR CODE HERE
	uuidWithHyphen := uuid.New()
	userID := strings.Replace(uuidWithHyphen.String(), "-", "", -1)

	//Create new verification token with the default token size (look at GetRandomBase62 and our constants)
	// YOUR CODE HERE
	verificationToken := GetRandomBase62(verifyTokenSize)

	//Store credentials in database
	_, err = DB.Query("INSERT INTO users(userId, username, email, hashedPassword, verifiedToken) VALUES(?, ?, ?, ?, ?)", userID, username, email, hashedPassword, verificationToken)
	// stmt, err := DB.Prepare("INSERT INTO users(userId, username, email, hashedPassword, verifiedToken) VALUES(?, ?, ?, ?)")
	//Check for errors in storing the credentials
	// YOUR CODE HERE
	if err != nil {
		log.Print(err.Error())
		http.Error(w, "Server error, unable to create your account.", 500)
	}

	//Generate an access token, expiry dates are in Unix time
	accessExpiresAt := time.Now().Add(DefaultAccessJWTExpiry)
	var accessToken string
	accessToken, err = setClaims(AuthClaims{
		UserID: userID,
		StandardClaims: jwt.StandardClaims{
			Subject:   "access",
			ExpiresAt: accessExpiresAt.Unix(),
			Issuer:    defaultJWTIssuer,
			IssuedAt:  time.Now().Unix(),
		},
	})

	//Check for error in generating an access token
	// YOUR CODE HERE
	if err != nil {
		http.Error(w, errors.New("error creating accessToken").Error(), http.StatusInternalServerError)
		log.Print(err.Error())
		return
	}

	//Set the cookie, name it "access_token"
	http.SetCookie(w, &http.Cookie{
		Name:    "access_token",
		Value:   accessToken,
		Expires: accessExpiresAt,
		// Leave these next three values commented for now
		// Secure: true,
		// HttpOnly: true,
		// SameSite: http.SameSiteNoneMode,
		Path: "/",
	})

	//Generate refresh token
	var refreshExpiresAt = time.Now().Add(DefaultRefreshJWTExpiry)
	var refreshToken string
	refreshToken, err = setClaims(AuthClaims{
		UserID: userID,
		StandardClaims: jwt.StandardClaims{
			Subject:   "refresh",
			ExpiresAt: refreshExpiresAt.Unix(),
			Issuer:    defaultJWTIssuer,
			IssuedAt:  time.Now().Unix(),
		},
	})

	if err != nil {
		http.Error(w, errors.New("error creating refreshToken").Error(), http.StatusInternalServerError)
		log.Print(err.Error())
		return
	}

	//set the refresh token ("refresh_token") as a cookie
	http.SetCookie(w, &http.Cookie{
		Name:    "refresh_token",
		Value:   refreshToken,
		Expires: refreshExpiresAt,
		Path:    "/",
	})

	// Send verification email
	err = SendEmail(credentials.Email, "Email Verification", "user-signup.html", map[string]interface{}{"Token": verificationToken})
	if err != nil {
		http.Error(w, errors.New("error sending verification email").Error(), http.StatusInternalServerError)
		log.Print(err.Error())
		return
	}

	w.WriteHeader(http.StatusCreated)
	return
}

func signin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "http://localhost:3000")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Credentials", "true")

	if (*r).Method == "OPTIONS" {
		return
	}

	//Store the credentials in a instance of Credentials
	// "YOUR CODE HERE"
	credentials := Credentials{}

	jsonDecoder := json.NewDecoder(r.Body)

	//Check for errors in storing credntials
	// "YOUR CODE HERE"
	err := jsonDecoder.Decode(&credentials)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	//Get the hashedPassword and userId of the user
	var hashedPassword, userID string
	err = DB.QueryRow("SELECT hashedPassword, userId FROM users WHERE username=?", credentials.Username).Scan(&hashedPassword, &userID)
	// process errors associated with emails
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, errors.New("this email is not associated with an account").Error(), http.StatusNotFound)
		} else {
			http.Error(w, errors.New("error retrieving information with this email").Error(), http.StatusInternalServerError)
			log.Print(err.Error())
		}
		return
	}

	// Check if hashed password matches the one corresponding to the email
	// "YOUR CODE HERE"
	err = bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(credentials.Password))

	//Check error in comparing hashed passwords
	// "YOUR CODE HERE"
	if err != nil {
		http.Redirect(w, r, "/login", 301)
		return
	}

	//Generate an access token  and set it as a cookie (Look at signup and feel free to copy paste!)
	// "YOUR CODE HERE"
	accessExpiresAt := time.Now().Add(DefaultAccessJWTExpiry)
	var accessToken string
	accessToken, err = setClaims(AuthClaims{
		UserID: userID,
		StandardClaims: jwt.StandardClaims{
			Subject:   "access",
			ExpiresAt: accessExpiresAt.Unix(),
			Issuer:    defaultJWTIssuer,
			IssuedAt:  time.Now().Unix(),
		},
	})

	if err != nil {
		http.Error(w, errors.New("error creating accessToken").Error(), http.StatusInternalServerError)
		log.Print(err.Error())
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:    "access_token",
		Value:   accessToken,
		Expires: accessExpiresAt,
		// Leave these next three values commented for now
		// Secure: true,
		// HttpOnly: true,
		// SameSite: http.SameSiteNoneMode,
		Path: "/",
	})

	//Generate a refresh token and set it as a cookie (Look at signup and feel free to copy paste!)
	// "YOUR CODE HERE"
	var refreshExpiresAt = time.Now().Add(DefaultRefreshJWTExpiry)
	var refreshToken string
	refreshToken, err = setClaims(AuthClaims{
		UserID: userID,
		StandardClaims: jwt.StandardClaims{
			Subject:   "refresh",
			ExpiresAt: refreshExpiresAt.Unix(),
			Issuer:    defaultJWTIssuer,
			IssuedAt:  time.Now().Unix(),
		},
	})

	if err != nil {
		http.Error(w, errors.New("error creating refreshToken").Error(), http.StatusInternalServerError)
		log.Print(err.Error())
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:    "refresh_token",
		Value:   refreshToken,
		Expires: refreshExpiresAt,
		Path:    "/",
	})

}

func logout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "http://localhost:3000")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")

	if (*r).Method == "OPTIONS" {
		return
	}

	// logging out causes expiration time of cookie to be set to now

	//Set the access_token and refresh_token to have an empty value and set their expiration date to anytime in the past
	var expiresAt = time.Now()
	http.SetCookie(w, &http.Cookie{Name: "access_token", Value: "", Expires: expiresAt})
	http.SetCookie(w, &http.Cookie{Name: "refresh_token", Value: "", Expires: expiresAt})
	return
}

func verify(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "localhost:3000")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Credentials", "true")

	if (*r).Method == "OPTIONS" {
		return
	}

	token, ok := r.URL.Query()["token"]
	// check that valid token exists
	if !ok || len(token[0]) < 1 {
		http.Error(w, errors.New("Url Param 'token' is missing").Error(), http.StatusInternalServerError)
		log.Print(errors.New("Url Param 'token' is missing").Error())
		return
	}

	//Obtain the user with the verifiedToken from the query parameter and set their verification status to the integer "1"
	_, err := DB.Exec("UPDATE users SET verified=? WHERE verifiedToken=?", 1, token)

	//Check for errors in executing the previous query
	// "YOUR CODE HERE"
	if err != nil {
		http.Error(w, errors.New("error checking verifiedToken").Error(), http.StatusBadRequest)
		log.Print(err.Error())
		return
	}

	return
}

func sendReset(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "localhost:3000")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Credentials", "true")

	if (*r).Method == "OPTIONS" {
		return
	}

	//Get the email from the body (decode into an instance of Credentials)
	// "YOUR CODE HERE"
	credentials := Credentials{}

	jsonDecoder := json.NewDecoder(r.Body)
	err := jsonDecoder.Decode(&credentials)
	//check for errors decoding the object
	// "YOUR CODE HERE"

	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	//check for other miscallenous errors that may occur
	//what is considered an invalid input for an email?
	// "YOUR CODE HERE"

	//generate reset token
	token := GetRandomBase62(resetTokenSize)

	//Obtain the user with the specified email and set their resetToken to the token we generated
	_, err = DB.Query("UPDATE users SET resetToken=? WHERE email=?", token, credentials.Email)

	//Check for errors executing the queries
	// "YOUR CODE HERE"
	if err != nil {
		http.Error(w, errors.New("error checking resetToken").Error(), http.StatusInternalServerError)
		log.Print(err.Error())
		return
	}

	// Send verification email
	err = SendEmail(credentials.Email, "BearChat Password Reset", "password-reset.html", map[string]interface{}{"Token": token})
	if err != nil {
		http.Error(w, errors.New("error sending verification email").Error(), http.StatusInternalServerError)
		log.Print(err.Error())
		return
	}
	return
}

func resetPassword(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "localhost:3000")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Credentials", "true")

	if (*r).Method == "OPTIONS" {
		return
	}

	//get token from query params
	token := r.URL.Query().Get("token")

	//get the username, email, and password from the body
	// "YOUR CODE HERE"
	credentials := Credentials{}

	jsonDecoder := json.NewDecoder(r.Body)
	err := jsonDecoder.Decode(&credentials)
	//Check for errors decoding the body
	// "YOUR CODE HERE"
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	//Check for invalid inputs, return an error if input is invalid
	// "YOUR CODE HERE"

	// email := credentials.Email
	username := credentials.Username
	password := credentials.Password
	var exists bool
	//check if the username and token pair exist
	err = DB.QueryRow("SELECT exists(SELECT * FROM users WHERE username=? AND resetToken=?)", username, token).Scan(&exists)

	//Check for errors executing the query
	// "YOUR CODE HERE"
	if err != nil {
		http.Error(w, errors.New("error checking username and verifiedToken").Error(), http.StatusInternalServerError)
		log.Print(err.Error())
		return
	}

	//Check exists boolean. Call an error if the username-token pair doesn't exist
	// "YOUR CODE HERE"
	if exists == false {
		http.Error(w, errors.New("this username and token don't exist").Error(), http.StatusNotFound)
		return
	}

	//Hash the new password
	// "YOUR CODE HERE"
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)

	//Check for errors in hashing the new password
	// "YOUR CODE HERE"
	if err != nil {
		http.Error(w, "Server error, unable to create your account.", 500)
	}

	//input new password and clear the reset token (set the token equal to empty string)
	_, err = DB.Exec("UPDATE users SET hashedPassword=?, verifiedToken=? WHERE username=?", hashedPassword, "", username)

	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		log.Print(err.Error())
	}

	//put the user in the redis cache to invalidate all current sessions (NOT IN SCOPE FOR PROJECT), leave this comment for future reference

	return
}
