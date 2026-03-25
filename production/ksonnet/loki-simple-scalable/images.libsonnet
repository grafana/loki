{
  _images+:: {
    loki: 'grafana/loki:v3.7.0',

    read: self.loki,
    write: self.loki,
  },
}
