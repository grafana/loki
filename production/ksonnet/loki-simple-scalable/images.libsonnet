{
  _images+:: {
    loki: 'grafana/loki:2.6.1',

    read: self.loki,
    write: self.loki,
  },
}
