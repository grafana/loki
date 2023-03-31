{
  _images+:: {
    loki: 'grafana/loki:2.7.5',

    read: self.loki,
    write: self.loki,
  },
}
