#!/bin/bash

cd "$(dirname "$0")"

set -o nounset \
    -o errexit \
    -o verbose \
    -o xtrace

# Generate CA key
openssl req -new -x509 \
  -out promtail-kafka-ca.pem \
  -keyout promtail-kafka-ca-key.pem \
  -days 36500 \
  -subj '/CN=ca.promtail.io/OU=TEST/O=Grafana/L=PaloAlto/S=Ca/C=US' \
  -passin pass:promtail \
  -passout pass:promtail

for i in broker producer consumer
do
	echo "[INFO] Creating ${i} certs"

	# Create keystores
	keytool -genkey -noprompt \
				 -alias "${i}" \
				 -dname "CN=${i}.promtail.io, OU=TEST, O=Grafana, L=PaloAlto, S=Ca, C=US" \
				 -keystore "kafka.${i}.keystore.jks" \
				 -keyalg RSA \
				 -storepass promtail \
				 -keypass promtail

	# Create CSR, sign the key and import back into keystore
	keytool -noprompt -keystore "kafka.${i}.keystore.jks" -alias "${i}" -certreq -file "${i}.csr" -storepass promtail -keypass promtail

	openssl x509 -req \
	  -CA promtail-kafka-ca.pem \
	  -CAkey promtail-kafka-ca-key.pem \
	  -in "${i}.csr" -out "${i}-ca-signed.crt" \
	  -days 36500 \
	  -CAcreateserial \
	  -passin pass:promtail

	keytool -noprompt -keystore "kafka.${i}.keystore.jks" \
	  -alias CARoot \
	  -import -file promtail-kafka-ca.pem \
	  -storepass promtail -keypass promtail

	keytool -noprompt -keystore "kafka.${i}.keystore.jks" \
	  -alias "${i}" \
	  -import -file "${i}-ca-signed.crt" \
	  -storepass promtail -keypass promtail

	# Create truststore and import the CA cert.
	keytool -noprompt -keystore "kafka.${i}.truststore.jks" \
	  -alias CARoot \
	  -import -file promtail-kafka-ca.pem \
	  -storepass promtail -keypass promtail

  # Convert jks to pem encoding
  keytool -noprompt -importkeystore \
    -srckeystore "kafka.${i}.keystore.jks" \
    -destkeystore "kafka.${i}.keystore.p12" \
    -deststoretype PKCS12 \
    -storepass promtail -keypass promtail \
    -srcstorepass promtail -deststorepass promtail
  openssl pkcs12 -in "kafka.${i}.keystore.p12" -nokeys \
    -out "kafka.${i}.keystore.cer.pem" \
    -passin pass:promtail -passout pass:promtail
  openssl pkcs12 -in "kafka.${i}.keystore.p12" -nodes -nocerts \
    -out "kafka.${i}.keystore.key.pem" \
    -passin pass:promtail -passout pass:promtail

  echo "promtail" > "${i}_sslkey_creds"
  echo "promtail" > "${i}_keystore_creds"
  echo "promtail" > "${i}_truststore_creds"
done