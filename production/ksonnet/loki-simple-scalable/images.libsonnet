{
  _images+:: {
    loki: 'grafana/loki:2.7.1',

    read: self.loki,
    write: self.loki,
  },
}
