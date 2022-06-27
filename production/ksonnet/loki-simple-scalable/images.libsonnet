{
  _images+:: {
    loki: 'grafana/loki:2.6.0',

    read: self.loki,
    write: self.loki,
  },
}
