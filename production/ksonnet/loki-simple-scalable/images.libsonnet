{
  _images+:: {
    loki: 'grafana/loki:2.7.2',

    read: self.loki,
    write: self.loki,
  },
}
