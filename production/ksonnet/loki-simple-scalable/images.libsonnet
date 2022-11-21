{
  _images+:: {
    loki: 'grafana/loki:2.7.0',

    read: self.loki,
    write: self.loki,
  },
}
