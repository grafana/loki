{
  _images+:: {
    loki: 'grafana/loki:2.5.0',

    read: self.loki,
    write: self.loki,
  },
}
