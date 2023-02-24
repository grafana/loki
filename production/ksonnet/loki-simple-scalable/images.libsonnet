{
  _images+:: {
    loki: 'grafana/loki:2.7.4',

    read: self.loki,
    write: self.loki,
  },
}
