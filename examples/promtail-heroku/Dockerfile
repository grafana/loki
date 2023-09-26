from grafana/promtail:main-c9ef062

# Copy the config.yml file we created in the step above, inside the container itself. This simplifies the 
# configuration of the Promtail instance.
COPY config.yml /etc/promtail/config.yml

# This three flags indicates promtail where the configurations is located, to interpolate the configuration
# file with environment variables (this is to avoid hardcoding values such as API Keys), and lastly, to print
# the whole configuration file to STDERR when starting the container (this is helpful for debugging).
CMD ["-config.file=/etc/promtail/config.yml", "-config.expand-env=true", "-print-config-stderr"]