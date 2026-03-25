{
  _images+:: {
    loki: 'grafana/loki:3.7.0',

    read: self.loki,
    write: self.loki,
  },
}
