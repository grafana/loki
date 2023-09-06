{
  _images+:: {
    loki: 'grafana/loki:2.9.0',

    read: self.loki,
    write: self.loki,
  },
}
